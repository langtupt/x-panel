"use client";
import { useEffect } from "react";
import { useRouter } from "next/navigation";
export default function PHPPage() {
  const router = useRouter();
  useEffect(() => router.replace("/applications/"), [router]);
  return <p>Đang chuyển đến Applications…</p>;
}
