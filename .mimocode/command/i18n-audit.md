---
description: "Audit locale files in the current branch against a reference branch. Shows added, removed, and changed keys across all 13 locale files. Use when the user asks 'check i18n keys vs main' or 'what changed in locales'"
---
# i18n-audit — audit locale keys across branches

Usage: `/i18n-audit` or `/i18n-audit <ref-branch>` (default: `origin/main`)

## Procedure

Run these steps in sequence from the project root:

1. Dump the reference branch's en-US to a temp file:
```bash
git show $REF:internal/web/translation/en-US.json > /tmp/ref-en-US.json
```
where `$REF` is the first argument or `origin/main` if none given. Verify it succeeded:
```bash
wc -c /tmp/ref-en-US.json && python3 -c "import json; json.load(open('/tmp/ref-en-US.json')); print('valid')"
```

2. Sync each locale file against the reference baseline:
```bash
python3 ~/.config/mimocode/tools/i18n-patch.py sync internal/web/translation/ar-EG.json internal/web/translation/en-US.json internal/web/translation/es-ES.json internal/web/translation/fa-IR.json internal/web/translation/id-ID.json internal/web/translation/ja-JP.json internal/web/translation/pt-BR.json internal/web/translation/ru-RU.json internal/web/translation/tr-TR.json internal/web/translation/uk-UA.json internal/web/translation/vi-VN.json internal/web/translation/zh-CN.json internal/web/translation/zh-TW.json --baseline /tmp/ref-en-US.json
```

`sync` output format: `LOCALE: +N added, -M removed, keys=K` for each file. Exit code 0 = all perfect match. Non-zero = drift.

3. If the user wants value-level diff (which keys changed, not just added/removed), run:
```bash
python3 << 'PYEOF'
import json, subprocess, sys
ref = sys.argv[1] if len(sys.argv)>1 else "origin/main"
LOCALES = ["ar-EG","en-US","es-ES","fa-IR","id-ID","ja-JP","pt-BR","ru-RU","tr-TR","uk-UA","vi-VN","zh-CN","zh-TW"]
cmd = ["git","show",f"{ref}:internal/web/translation/en-US.json"]
ref_data = json.loads(subprocess.run(cmd, capture_output=True, text=True).stdout)

with open("internal/web/translation/en-US.json") as f:
    cur_data = json.load(f)

def flatten(d, prefix=""):
    for k,v in d.items():
        full = f"{prefix}.{k}" if prefix else k
        if isinstance(v, dict): yield from flatten(v, full)
        else: yield full, v

ref_flat = dict(flatten(ref_data))
cur_flat = dict(flatten(cur_data))
added = set(cur_flat) - set(ref_flat)
removed = set(ref_flat) - set(cur_flat)
changed = {k for k in ref_flat & cur_flat if ref_flat[k] != cur_flat[k]}
if added: print(f"ADDED ({len(added)}):", *sorted(added), sep="\n  ")
if removed: print(f"REMOVED ({len(removed)}):", *sorted(removed), sep="\n  ")
if changed:
    print(f"CHANGED ({len(changed)}):")
    for k in sorted(changed): print(f"  {k}: {ref_flat[k]!r} → {cur_flat[k]!r}")
if not (added or removed or changed): print("en-US has zero changes from reference — clean.")
PYEOF
```

## Output interpretation

- **+N added**: keys present in this locale file that are NOT in the reference baseline. These are branch additions.
- **−N removed**: keys present in reference baseline that are MISSING from this locale file. These were deleted or never added in the branch.
- **CHANGED**: keys that exist in both but have different values between branch and reference.

A clean audit shows `en-US` with 0 added / 0 removed AND all 12 other locales matching en-US exactly (exit 0). Any non-zero exit code means drift that needs attention before merging.

## Related

- `i18n-add-key` skill: add individual keys with translations
- `~/.config/mimocode/tools/i18n-patch.py sync`: per-file key-set diff
- `internal/web/translation/`: 13 locale JSON files