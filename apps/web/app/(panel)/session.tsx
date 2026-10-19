"use client";

import { createContext, useContext } from "react";
import type { Me } from "@/lib/api";

export const SessionContext = createContext<Me | null>(null);

export function useSession(): Me {
  const u = useContext(SessionContext);
  if (!u) throw new Error("useSession outside panel layout");
  return u;
}
