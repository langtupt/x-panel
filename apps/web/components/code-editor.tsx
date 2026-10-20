"use client";

import { useEffect, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import type { Extension } from "@codemirror/state";
import { LanguageDescription } from "@codemirror/language";
import { languages } from "@codemirror/language-data";

/** Code editor with syntax highlighting chosen from the file name. */
export default function CodeEditor({
  path,
  value,
  onChange,
  onSave,
}: {
  path: string;
  value: string;
  onChange: (v: string) => void;
  onSave: () => void;
}) {
  const [lang, setLang] = useState<Extension[]>([]);
  const [dark, setDark] = useState(false);

  useEffect(() => {
    const name = path.split("/").pop() ?? "";
    // .env, .htaccess and friends: plain "properties" style highlighting.
    const desc =
      LanguageDescription.matchFilename(languages, name) ??
      (/^\.(env|htaccess|user\.ini)/.test(name) ? LanguageDescription.matchLanguageName(languages, "properties") : null);
    let alive = true;
    if (desc) desc.load().then((s) => alive && setLang([s]));
    else setLang([]);
    return () => { alive = false; };
  }, [path]);

  useEffect(() => {
    const m = window.matchMedia("(prefers-color-scheme: dark)");
    setDark(m.matches || document.documentElement.classList.contains("dark"));
  }, []);

  return (
    <CodeMirror
      value={value}
      height="100%"
      className="h-full text-[13px]"
      theme={dark ? "dark" : "light"}
      extensions={lang}
      onChange={onChange}
      basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: true, searchKeymap: true }}
      onKeyDown={(e) => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
          e.preventDefault();
          onSave();
        }
      }}
    />
  );
}
