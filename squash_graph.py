#!/usr/bin/env python3
"""
squash_graph.py — строит граф squash-кандидатов на основе реальных
пересечений изменяемых строк между коммитами в заданном диапазоне.

Логика:
1. Для каждого коммита в диапазоне парсим `git diff -U0 <parent> <commit>`
   и извлекаем изменённые line-ranges по файлам (из @@ -a,b +c,d @@).
2. Строим граф "line ownership": какой коммит последним трогал каждую
   строку каждого файла на момент HEAD (через `git blame`).
3. Два типа рёбер между коммитами i < j (i раньше j):
   - blame-dep (направленное i -> j): j меняет строки, которые по blame
     на HEAD принадлежат i. Squash j без i (или в другом порядке)
     невозможен — история строки прервётся некорректно. Это ЖЁСТКАЯ
     зависимость: j обязан идти после i при любом squash-плане, но сам
     squash i+j в один коммит АВТОМАТИЧЕСКИ безопасен (git squash就是
     "применить оба diff'а последовательно", а j и так строится поверх i).
   - line-overlap (ненаправленное i <-> j): диапазоны строк i и j
     пересекаются/касаются в одном файле, но НЕ обязательно через
     финальный blame (например, i менял строки, которые потом изменил
     j, и на HEAD владелец уже j — прямой blame-dep не всплывёт, но
     историческое пересечение было). Такие пары — кандидаты на CONFLICT
     при интерактивном squash: git скорее всего попросит ручное слияние
     хантов, т.к. диапазоны физически перекрывались.
4. Связные компоненты графа (по объединению обоих типов рёбер) —
   атомарные squash-блоки: коммиты внутри компоненты нельзя произвольно
   тасовать/выдёргивать по одному, не разбирая конфликты руками.
   Компоненты без рёбер друг с другом — независимы, сквошить их
   относительно друг друга можно в любом порядке.
5. Внутри компоненты коммиты сортируются топологически по blame-dep
   рёбрам (i -> j: i должен быть раньше j), с хронологическим порядком
   как tie-breaker. Если топосорт невозможен (цикл в blame-dep — в
   линейной истории такого быть не должно, но при --context>0 или
   переименованиях файлов теоретически возможно) — печатается
   предупреждение и используется исходный хронологический порядок.
6. Каждая компонента размечается как SAFE (только blame-dep рёбра —
   squash технически бесконфликтен, порядок уже корректный) или
   CONFLICT (есть хотя бы одно line-overlap ребро без соответствующего
   blame-dep — вероятен ручной мёрж хантов).

Использование:
    python3 squash_graph.py <rev-range> [--repo PATH] [--context N] [--dot OUT.dot] [--json OUT.json] [--rebase-todo OUT.txt]

Пример:
    python3 squash_graph.py HEAD~20..HEAD --dot graph.dot --json graph.json --rebase-todo todo.txt

Требует git >= 2.x в PATH. Не изменяет репозиторий (только чтение).
"""

import argparse
import json
import re
import subprocess
import sys
from collections import defaultdict, deque


HUNK_RE = re.compile(r"^@@ -(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s@@")
DIFF_FILE_RE = re.compile(r"^diff --git a/(.*) b/(.*)$")


def run(args, cwd=None):
    res = subprocess.run(
        ["git", *args], cwd=cwd, capture_output=True, text=True, check=False
    )
    if res.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} failed:\n{res.stderr}")
    return res.stdout


def get_commit_list(rev_range, repo):
    # Старые коммиты первыми — важно для последовательного blame-анализа
    out = run(["log", "--reverse", "--pretty=format:%H\x1f%s", rev_range], cwd=repo)
    commits = []
    for line in out.splitlines():
        if not line.strip():
            continue
        sha, subject = line.split("\x1f", 1)
        commits.append({"sha": sha, "subject": subject})
    return commits


def get_touched_ranges(sha, repo):
    """
    Возвращает {filepath: [(start_line, end_line_inclusive), ...]}
    для строк, добавленных/изменённых этим коммитом в НОВОЙ версии файла
    (координаты +c,d стороны хунка). Файлы, которые коммит только удалил
    целиком, пропускаются (нет строк в новой версии, отслеживать нечего).
    """
    diff = run(
        ["diff", "-U0", "--no-color", f"{sha}^!", "--"], cwd=repo
    )
    ranges = defaultdict(list)
    current_file = None
    for line in diff.splitlines():
        m = DIFF_FILE_RE.match(line)
        if m:
            current_file = m.group(2)
            continue
        m = HUNK_RE.match(line)
        if m and current_file:
            new_start = int(m.group(3))
            new_len = int(m.group(4)) if m.group(4) is not None else 1
            if new_len == 0:
                # чисто удаление на этой стороне — нет новых строк для blame
                continue
            ranges[current_file].append((new_start, new_start + new_len - 1))
    return ranges


_blame_cache = {}  # filepath -> {line_no: sha} or None if unavailable


def get_file_blame_map(filepath, head_sha, repo):
    """
    Один вызов `git blame` на весь файл на HEAD, закешированный.
    Возвращает {line_no: sha} или None, если файл недоступен на HEAD
    (переименован/удалён и т.п. — тогда просто нет данных по нему).
    """
    if filepath in _blame_cache:
        return _blame_cache[filepath]
    try:
        out = run(
            ["blame", "--porcelain", head_sha, "--", filepath],
            cwd=repo,
        )
    except RuntimeError:
        _blame_cache[filepath] = None
        return None
    line_map = {}
    cur_sha = None
    cur_line = None
    for line in out.splitlines():
        m = re.match(r"^([0-9a-f]{40})\s+\d+\s+(\d+)", line)
        if m:
            cur_sha = m.group(1)
            cur_line = int(m.group(2))
            line_map[cur_line] = cur_sha
    _blame_cache[filepath] = line_map
    return line_map


def get_blame_owners(filepath, line_range, head_sha, repo):
    """
    Для диапазона строк в файле на HEAD возвращает множество sha коммитов,
    которым эти строки принадлежат по blame (кто их последний раз менял).
    Использует закешированную по файлу blame-карту вместо отдельного
    вызова git blame на каждый диапазон.
    """
    line_map = get_file_blame_map(filepath, head_sha, repo)
    if not line_map:
        return set()
    start, end = line_range
    owners = set()
    for ln in range(start, end + 1):
        sha = line_map.get(ln)
        if sha:
            owners.add(sha)
    return owners


def ranges_overlap_or_touch(r1, r2, context):
    a_start, a_end = r1
    b_start, b_end = r2
    return not (a_end + context < b_start or b_end + context < a_start)


DEFAULT_EXCLUDE_PATTERNS = [
    r"generated/",
    r"openapi\.json$",
    r"mockServiceWorker\.js$",
    r"translation/.*\.json$",
    r"\.lock$",
    r"package-lock\.json$",
    r"pnpm-lock\.yaml$",
    r"go\.sum$",
]


def is_excluded(filepath, patterns):
    return any(re.search(p, filepath) for p in patterns)


def build_graph(commits, repo, context, exclude_patterns, min_weight):
    """
    Возвращает:
      pair_evidence: {(i,j) i<j -> {"weight": int, "files": {file: {reasons}}}}
      strong_edges:  set of (i,j) i<j с weight >= min_weight
      dep_edges:     set of (i,j) i<j, где i -> j есть направленная
                     blame-dep зависимость (i должен идти раньше j)
      index_by_sha:  sha -> индекс в commits
    """
    n = len(commits)
    index_by_sha = {c["sha"]: i for i, c in enumerate(commits)}
    head_sha = commits[-1]["sha"]

    touched = {}  # sha -> {file: [ranges]}
    for idx, c in enumerate(commits):
        print(f"  [{idx+1}/{n}] diff {c['sha'][:8]}  {c['subject'][:60]}", file=sys.stderr)
        raw = get_touched_ranges(c["sha"], repo)
        touched[c["sha"]] = {
            f: ranges for f, ranges in raw.items() if not is_excluded(f, exclude_patterns)
        }
        excluded_count = len(raw) - len(touched[c["sha"]])
        if excluded_count:
            print(f"      (пропущено {excluded_count} шумовых/сгенерированных файлов)", file=sys.stderr)

    # pair_evidence[(i,j)] i<j -> {"weight": int, "files": {file: {reasons}}}
    pair_evidence = defaultdict(lambda: {"weight": 0, "files": defaultdict(set)})
    dep_edges = set()  # (i, j) i<j, направленное i -> j (blame-dep)

    def add_edge(i, j, filename, reason):
        lo, hi = (i, j) if i < j else (j, i)
        ev = pair_evidence[(lo, hi)]
        ev["weight"] += 1
        ev["files"][filename].add(reason)

    # 1) line-overlap: прямое историческое пересечение диапазонов по файлу
    per_file_touches = defaultdict(list)  # file -> [(commit_idx, range)]
    for i, c in enumerate(commits):
        for f, ranges in touched[c["sha"]].items():
            for r in ranges:
                per_file_touches[f].append((i, r))

    for f, touches in per_file_touches.items():
        for a in range(len(touches)):
            i, r1 = touches[a]
            for b in range(a + 1, len(touches)):
                j, r2 = touches[b]
                if i == j:
                    continue
                if ranges_overlap_or_touch(r1, r2, context):
                    add_edge(i, j, f, "line-overlap")

    # 2) blame-dep: более поздний коммит меняет строки, принадлежащие
    #    (по blame на HEAD) более раннему коммиту в наборе.
    #    Направление всегда earlier -> later, т.к. blame берётся на HEAD
    #    и владелец строки не может быть "из будущего" относительно
    #    коммита, который эту строку правит (в линейной истории).
    unique_files = sorted({f for c in commits for f in touched[c["sha"]]})
    print(f"Blame по {len(unique_files)} уникальным файлам (после исключений)...", file=sys.stderr)
    for fi, f in enumerate(unique_files):
        print(f"  [{fi+1}/{len(unique_files)}] blame {f}", file=sys.stderr)
        get_file_blame_map(f, head_sha, repo)

    for i, c in enumerate(commits):
        sha = c["sha"]
        for f, ranges in touched[sha].items():
            for r in ranges:
                owners = get_blame_owners(f, r, head_sha, repo)
                for owner_sha in owners:
                    if owner_sha == sha:
                        continue
                    j = index_by_sha.get(owner_sha)
                    if j is None or j == i:
                        continue
                    add_edge(i, j, f, "blame-dep")
                    lo, hi = (i, j) if i < j else (j, i)
                    dep_edges.add((lo, hi))  # lo раньше hi хронологически

    strong_edges = set()
    for (i, j), ev in pair_evidence.items():
        if ev["weight"] >= min_weight:
            strong_edges.add((i, j))

    return pair_evidence, strong_edges, dep_edges, index_by_sha


def union_find(n, edges):
    parent = list(range(n))

    def find(x):
        while parent[x] != x:
            parent[x] = parent[parent[x]]
            x = parent[x]
        return x

    def union(a, b):
        ra, rb = find(a), find(b)
        if ra != rb:
            parent[ra] = rb

    for a, b in edges:
        union(a, b)
    groups = defaultdict(list)
    for i in range(n):
        groups[find(i)].append(i)
    return list(groups.values())


def topo_sort_component(indices, dep_edges):
    """
    Топосорт по направленным blame-dep рёбрам (i -> j значит i раньше j),
    ограниченным подмножеством indices. Kahn's algorithm; при равном
    in-degree выбирается меньший хронологический индекс (стабильность).
    Возвращает (order, is_valid). is_valid=False, если обнаружен цикл —
    тогда order — это исходный хронологический порядок как fallback.
    """
    idx_set = set(indices)
    adj = defaultdict(list)
    indeg = {i: 0 for i in indices}
    for (i, j) in dep_edges:
        if i in idx_set and j in idx_set:
            adj[i].append(j)
            indeg[j] += 1

    ready = sorted([i for i in indices if indeg[i] == 0])
    ready = deque(ready)
    order = []
    while ready:
        # достаём с наименьшим индексом среди готовых — стабильный порядок
        cur = min(ready)
        ready.remove(cur)
        order.append(cur)
        for nxt in sorted(adj[cur]):
            indeg[nxt] -= 1
            if indeg[nxt] == 0:
                ready.append(nxt)

    if len(order) != len(indices):
        # цикл — не должно случаться в норме, но не падаем
        return sorted(indices), False
    return order, True


def classify_component(comp, dep_edges, pair_evidence, min_weight):
    """
    SAFE: все сильные рёбра внутри компоненты — blame-dep (направленные).
          Squash технически бесконфликтен при условии топологического
          порядка — каждый коммит просто "достраивает" предыдущий.
    CONFLICT: есть хотя бы одна сильная пара с line-overlap, для которой
          нет "перекрывающего" blame-dep в ту же сторону — вероятен
          ручной мёрж хантов при squash/rebase.
    """
    comp_set = set(comp)
    has_conflict = False
    for i in comp:
        for j in comp:
            if j <= i:
                continue
            ev = pair_evidence.get((i, j))
            if not ev or ev["weight"] < min_weight:
                continue
            reasons = set()
            for rs in ev["files"].values():
                reasons |= rs
            if "line-overlap" in reasons and (i, j) not in dep_edges:
                has_conflict = True
    return "CONFLICT" if has_conflict else "SAFE"


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("rev_range", help="напр. HEAD~20..HEAD или main..feature")
    ap.add_argument("--repo", default=".", help="путь к репозиторию")
    ap.add_argument("--context", type=int, default=0, help="сколько строк зазора считать 'касанием' (default 0 = только реальное пересечение)")
    ap.add_argument("--min-weight", type=int, default=2, help="минимум пересечений/зависимостей между парой, чтобы считать их одним squash-блоком (default 2)")
    ap.add_argument("--exclude", action="append", default=[], help="доп. regex-паттерн пути файла для исключения (можно несколько раз)")
    ap.add_argument("--no-default-excludes", action="store_true", help="не использовать встроенный список исключений (generated/, lock-файлы, переводы, openapi.json)")
    ap.add_argument("--dot", help="путь для вывода Graphviz .dot")
    ap.add_argument("--json", help="путь для вывода JSON")
    ap.add_argument("--rebase-todo", help="путь для вывода готового rebase todo-списка (pick/squash)")
    ap.add_argument("--show-matrix", action="store_true", help="напечатать полную матрицу весов между всеми парами коммитов (включая слабые)")
    args = ap.parse_args()

    exclude_patterns = list(args.exclude)
    if not args.no_default_excludes:
        exclude_patterns += DEFAULT_EXCLUDE_PATTERNS

    commits = get_commit_list(args.rev_range, args.repo)
    if len(commits) < 2:
        print("Меньше двух коммитов в диапазоне — нечего анализировать.", file=sys.stderr)
        sys.exit(1)

    print(f"Анализирую {len(commits)} коммитов...", file=sys.stderr)
    pair_evidence, strong_edges, dep_edges, index_by_sha = build_graph(
        commits, args.repo, args.context, exclude_patterns, args.min_weight
    )

    components = union_find(len(commits), strong_edges)
    components.sort(key=lambda g: min(g))

    result_components = []  # [(order, is_valid_topo, classification)]
    for comp in components:
        order, is_valid = topo_sort_component(comp, dep_edges)
        cls = classify_component(comp, dep_edges, pair_evidence, args.min_weight) if len(comp) > 1 else "SINGLE"
        result_components.append((order, is_valid, cls))

    # ---- текстовый вывод ----
    print()
    print(f"(порог склейки: min-weight={args.min_weight}, context={args.context}, исключено паттернов: {len(exclude_patterns)})")
    print()
    for ci, (order, is_valid, cls) in enumerate(result_components):
        if len(order) == 1:
            c = commits[order[0]]
            print(f"[independent] {c['sha'][:8]}  {c['subject']}")
            continue
        warn = "" if is_valid else "  [!] цикл в blame-dep, порядок = хронологический fallback"
        print(f"=== squash-кандидат #{ci+1} ({len(order)} коммитов, {cls}){warn} ===")
        for i in order:
            c = commits[i]
            links = []
            for j in order:
                if j == i:
                    continue
                lo, hi = (i, j) if i < j else (j, i)
                ev = pair_evidence.get((lo, hi))
                if ev and ev["weight"] >= args.min_weight:
                    reasons = set()
                    for rs in ev["files"].values():
                        reasons |= rs
                    tag = "dep" if (lo, hi) in dep_edges and "line-overlap" not in reasons else \
                        ("dep+overlap" if (lo, hi) in dep_edges else "overlap")
                    files_str = ", ".join(sorted(ev["files"].keys())[:3])
                    links.append(f"{commits[j]['sha'][:8]}({tag}, w={ev['weight']}: {files_str})")
            link_str = f"  <- связан с: {'; '.join(links)}" if links else ""
            print(f"  {c['sha'][:8]}  {c['subject']}{link_str}")
        print()

    if args.show_matrix:
        print("=== Полная матрица связей (все пары с весом > 0) ===")
        for (i, j), ev in sorted(pair_evidence.items(), key=lambda kv: -kv[1]["weight"]):
            mark = "STRONG" if ev["weight"] >= args.min_weight else "weak  "
            files_str = ", ".join(sorted(ev["files"].keys()))
            print(f"  [{mark}] w={ev['weight']:3d}  {commits[i]['sha'][:8]} <-> {commits[j]['sha'][:8]}  files: {files_str}")
        print()

    # ---- dot ----
    if args.dot:
        with open(args.dot, "w") as f:
            f.write("digraph squash {\n  rankdir=TB;\n  node [shape=box, fontsize=10];\n")
            for i, c in enumerate(commits):
                label = c["subject"].replace('"', '\\"')[:50]
                f.write(f'  n{i} [label="{c["sha"][:8]}\\n{label}"];\n')
            for (i, j), ev in pair_evidence.items():
                if ev["weight"] >= args.min_weight:
                    color = "blue" if (i, j) in dep_edges else "red"
                    style = "dir=forward" if (i, j) in dep_edges else "dir=none"
                    f.write(f'  n{i} -> n{j} [color={color}, {style}, penwidth={min(ev["weight"],5)}, label="w={ev["weight"]}"];\n')
                else:
                    f.write(f'  n{i} -> n{j} [color=gray, dir=none, style=dotted];\n')
            f.write("}\n")
        print(f"Graphviz dot записан в {args.dot} (синий=blame-dep направленный, красный=line-overlap)", file=sys.stderr)

    # ---- json ----
    if args.json:
        data = {
            "commits": [{"sha": c["sha"], "subject": c["subject"]} for c in commits],
            "pair_evidence": [
                {
                    "i": i, "j": j,
                    "sha_i": commits[i]["sha"], "sha_j": commits[j]["sha"],
                    "weight": ev["weight"],
                    "strong": ev["weight"] >= args.min_weight,
                    "directed_dep": (i, j) in dep_edges,
                    "files": {f: sorted(list(reasons)) for f, reasons in ev["files"].items()},
                }
                for (i, j), ev in pair_evidence.items()
            ],
            "components": [
                {
                    "order": order,
                    "shas": [commits[i]["sha"] for i in order],
                    "topo_valid": is_valid,
                    "classification": cls,
                }
                for order, is_valid, cls in result_components
            ],
            "params": {"context": args.context, "min_weight": args.min_weight, "exclude_patterns": exclude_patterns},
        }
        with open(args.json, "w") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
        print(f"JSON записан в {args.json}", file=sys.stderr)

    # ---- rebase todo ----
    if args.rebase_todo:
        lines = []
        lines.append(f"# squash plan для {args.rev_range}")
        lines.append(f"# git rebase -i {commits[0]['sha']}^")
        lines.append("#")
        lines.append("# SAFE   — блок бесконфликтен при данном топологическом порядке")
        lines.append("# CONFLICT — вероятен ручной мёрж хантов, проверь диффы вручную перед squash")
        lines.append("#")
        for ci, (order, is_valid, cls) in enumerate(result_components):
            if len(order) == 1:
                c = commits[order[0]]
                lines.append(f"pick {c['sha'][:8]} {c['subject']}")
                continue
            lines.append(f"# --- блок #{ci+1}: {cls}{'  [!] топосорт невалиден, проверь вручную' if not is_valid else ''} ---")
            for pos, i in enumerate(order):
                c = commits[i]
                verb = "pick" if pos == 0 else "squash"
                lines.append(f"{verb} {c['sha'][:8]} {c['subject']}")
        with open(args.rebase_todo, "w") as f:
            f.write("\n".join(lines) + "\n")
        print(f"Rebase todo записан в {args.rebase_todo}", file=sys.stderr)


if __name__ == "__main__":
    main()