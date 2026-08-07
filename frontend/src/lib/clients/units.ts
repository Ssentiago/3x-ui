export const bytesPerGB = 1 << 30;

export function bytesToGB(bytes: number): number {
  if (!bytes || bytes <= 0) return 0;
  return Math.round((bytes / bytesPerGB) * 100) / 100;
}

export function gbToBytes(gb: number | undefined): number {
  if (!gb || gb <= 0) return 0;
  return Math.round(gb * bytesPerGB);
}
