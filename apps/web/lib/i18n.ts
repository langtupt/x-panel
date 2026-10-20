import catalog from "./locales/en.json";

export type Locale = "vi" | "en";
let locale: Locale = "vi";
const messages: Record<string, string> = catalog;

export function setLocale(value: Locale) {
  locale = value;
}
export function localeTag() {
  return locale === "en" ? "en-US" : "vi-VN";
}

/** Translate display text only. API identifiers and stored values stay unchanged. */
export function t<T>(value: T, params?: unknown[]): T {
  if (Array.isArray(value)) return value.map((item) => t(item)) as T;
  if (typeof value !== "string") return value;
  let result = value as string;
  if (locale === "en") {
    const key = value.trim().replace(/\s+/g, " ");
    const translated = messages[key];
    if (translated) result = value.replace(value.trim(), translated);
  }
  if (params)
    result = result.replace(/\{(\d+)\}/g, (match, index) =>
      String(params[Number(index)] ?? match),
    );
  return result as T;
}
