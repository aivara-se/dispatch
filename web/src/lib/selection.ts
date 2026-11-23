export function pick(
  picked: { id: string; n: number } | null,
  id: string,
  failing: number | null,
): number | null {
  return picked?.id === id ? picked.n : failing;
}
