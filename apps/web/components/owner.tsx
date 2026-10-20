"use client";
import { t as translate } from "@/lib/i18n";

import { useEffect, useState } from "react";
import { api, type User } from "@/lib/api";

/** Hosting accounts a manager can act for (users only see themselves). */
export function useHostingAccounts(manager: boolean) {
  const [owners, setOwners] = useState<User[]>([]);
  useEffect(() => {
    if (!manager) return;
    api<{ users: User[] }>("/users")
      .then((r) =>
        setOwners(
          r.users.filter((u) => u.role === "user" && u.linux_uid !== null),
        ),
      )
      .catch(() => setOwners([]));
  }, [manager]);
  return owners;
}

export function OwnerSelect({
  owners,
  value,
  onChange,
}: {
  owners: User[];
  value: number | null;
  onChange: (id: number | null) => void;
}) {
  return (
    <select
      className="input w-auto min-w-48"
      value={value ?? ""}
      onChange={(e) => onChange(Number(e.target.value) || null)}
    >
      {owners.length === 0 && (
        <option value="">
          {translate("Chưa có tài khoản người dùng nào")}
        </option>
      )}
      {owners.map((u) => (
        <option key={u.id} value={u.id}>
          {u.username}
        </option>
      ))}
    </select>
  );
}
