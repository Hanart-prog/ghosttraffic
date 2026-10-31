export function ms(value: number | null | undefined): string {
  if (value === null || value === undefined) return "n/a";
  return `${value.toFixed(value < 10 ? 2 : 1)} ms`;
}

export function words(value: string): string {
  const text = value.replaceAll("_", " ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

// The API's "none" means no error, which reads like a failure on screen.
export function outcome(category: string): string {
  return category === "none" ? "Completed" : words(category);
}

export function when(iso: string): string {
  return new Date(iso).toLocaleString();
}
