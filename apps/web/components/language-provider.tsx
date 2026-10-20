"use client";

import {
  createContext,
  Fragment,
  useContext,
  useEffect,
  useState,
} from "react";
import { type Locale, setLocale } from "@/lib/i18n";

const LanguageContext = createContext<{
  locale: Locale;
  change: (value: Locale) => void;
}>({ locale: "vi", change: () => {} });

export function LanguageProvider({ children }: { children: React.ReactNode }) {
  const [locale, update] = useState<Locale>("vi");
  function apply(value: Locale) {
    setLocale(value);
    document.documentElement.lang = value;
    update(value);
  }
  useEffect(() => {
    try {
      apply(localStorage.getItem("xpanel.language") === "en" ? "en" : "vi");
    } catch {
      apply("vi");
    }
    const sync = (event: StorageEvent) => {
      if (event.key === "xpanel.language")
        apply(event.newValue === "en" ? "en" : "vi");
    };
    window.addEventListener("storage", sync);
    return () => window.removeEventListener("storage", sync);
  }, []);
  function change(value: Locale) {
    if (value === locale) return;
    const proceed = () => {
      try {
        localStorage.setItem("xpanel.language", value);
      } catch {}
      apply(value);
    };
    if (
      window.dispatchEvent(
        new CustomEvent("xpanel:language-change", {
          cancelable: true,
          detail: { proceed },
        }),
      )
    )
      proceed();
  }

  return (
    <LanguageContext.Provider value={{ locale, change }}>
      <Fragment key={locale}>{children}</Fragment>
    </LanguageContext.Provider>
  );
}

function Flag({ country }: { country: Locale }) {
  return country === "vi" ? (
    <svg
      viewBox="0 0 30 20"
      className="h-4 w-6 overflow-hidden"
      aria-hidden="true"
    >
      <path fill="#da251d" d="M0 0h30v20H0z" />
      <path
        fill="#ff0"
        d="m15 4 1.4 4.3h4.5l-3.6 2.6 1.4 4.3-3.7-2.7-3.7 2.7 1.4-4.3-3.6-2.6h4.5z"
      />
    </svg>
  ) : (
    <svg
      viewBox="0 0 30 20"
      className="h-4 w-6 overflow-hidden"
      aria-hidden="true"
    >
      <path fill="#012169" d="M0 0h30v20H0z" />
      <path stroke="#fff" strokeWidth="5" d="m0 0 30 20M30 0 0 20" />
      <path stroke="#c8102e" strokeWidth="2" d="m0 0 30 20M30 0 0 20" />
      <path stroke="#fff" strokeWidth="7" d="M15 0v20M0 10h30" />
      <path stroke="#c8102e" strokeWidth="4" d="M15 0v20M0 10h30" />
    </svg>
  );
}

export function LanguageSwitcher() {
  const { locale, change } = useContext(LanguageContext);
  return (
    <div
      role="group"
      aria-label={locale === "en" ? "Language" : "Ngôn ngữ"}
      className="inline-flex shrink-0 items-center gap-3"
    >
      {(["vi", "en"] as const).map((value) => (
        <button
          key={value}
          type="button"
          onClick={() => change(value)}
          aria-pressed={locale === value}
          aria-label={value === "vi" ? "Tiếng Việt" : "English"}
          title={value === "vi" ? "Tiếng Việt" : "English"}
          className={`inline-flex h-8 items-center justify-center transition-opacity focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${locale === value ? "opacity-100" : "opacity-50 hover:opacity-100"}`}
        >
          <Flag country={value} />
        </button>
      ))}
    </div>
  );
}
