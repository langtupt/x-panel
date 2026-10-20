import { strict as assert } from "node:assert";
import { test, afterEach } from "node:test";
import { t, setLocale, localeTag } from "../apps/web/lib/i18n";
import messages from "../apps/web/lib/locales/en.json";

afterEach(() => setLocale("vi"));
test("Vietnamese remains the default and English can be switched back", () => {
  assert.equal(t("Tổng quan"), "Tổng quan");
  setLocale("en");
  assert.equal(t("Tổng quan"), "Overview");
  assert.equal(localeTag(), "en-US");
  setLocale("vi");
  assert.equal(t("Tổng quan"), "Tổng quan");
  assert.equal(localeTag(), "vi-VN");
});
test("parameters are interpolated without translating user data", () => {
  setLocale("en");
  assert.equal(t("Xóa website {0}", ["example.test"]), "Delete website example.test");
  assert.equal(t(" /api/apps/php "), " /api/apps/php ");
  assert.equal(t(5433), 5433);
  const object = {name:"Tổng quan"};
  assert.equal(t(object), object);
});
test("every translation preserves interpolation parameters", () => {
  for (const [key, value] of Object.entries(messages)) {
    assert.deepEqual(key.match(/\{\d+\}/g)?.sort() ?? [], value.match(/\{\d+\}/g)?.sort() ?? [], key);
    assert.ok(value.trim(), key);
  }
});
