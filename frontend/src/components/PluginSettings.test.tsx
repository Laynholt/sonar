// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "@/lib/i18n";
import type { plugin } from "../../wailsjs/go/models";
import { PluginSettings } from "./PluginSettings";
import { button, change, mount } from "../test/render";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));

const field = (f: Partial<plugin.Field>) => ({ key: "", type: "text", label: {}, hint: {}, default: null, options: [], action: "", ...f }) as plugin.Field;
const FIELDS = [
    field({ key: "name", type: "text", label: { en: "Name", ru: "Имя" }, hint: { en: "Your name", ru: "Ваше имя" } }),
    field({ key: "count", type: "number", label: { en: "Count" } }),
    field({ key: "single", type: "switch", label: { en: "One genre", ru: "Один жанр" } }),
    field({ key: "mode", type: "select", label: { en: "Mode" }, options: [{ value: 1, label: { en: "Fast", ru: "Быстро" } }, { value: 2, label: { en: "Slow" } }] as plugin.Option[] }),
    field({ key: "check", type: "button", label: { en: "Pass the check", ru: "Пройти проверку" }, action: "check" }),
    field({ key: "about", type: "note", label: { en: "Read me", ru: "Прочтите" } }),
];
const VALUES = { name: "Ann", count: 3, single: true, mode: 2 };

let view: Awaited<ReturnType<typeof mount>>;
const onChange = vi.fn();
const onAction = vi.fn();
const render = (lang: string, fields = FIELDS, values: Record<string, unknown> = VALUES) =>
    mount(<PluginSettings fields={fields} values={values} lang={lang} onChange={onChange} onAction={onAction}/>);

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
});
afterEach(async () => { await view?.unmount(); setLanguage("en"); vi.unstubAllGlobals(); });

describe("PluginSettings", () => {
    it("renders every field type with its value", async () => {
        view = await render("en");
        const text = view.container.textContent!;
        for (const label of ["Name", "Your name", "Count", "One genre", "Mode", "Read me"]) expect(text).toContain(label);
        expect((view.container.querySelector('input[type="text"]') as HTMLInputElement).value).toBe("Ann");
        expect((view.container.querySelector('input[type="number"]') as HTMLInputElement).value).toBe("3");
        expect(view.container.querySelector('[role="switch"]')!.getAttribute("aria-checked")).toBe("true");
        const select = view.container.querySelector("select")!;
        expect(select.value).toBe("2");
        expect([...select.options].map((o) => o.textContent)).toEqual(["Fast", "Slow"]);
        expect(button(view.container, "Pass the check")).toBeTruthy();
    });

    it("shows a plugin's labels in Russian when the UI is Russian", async () => {
        setLanguage("ru");
        view = await render("ru");
        const text = view.container.textContent!;
        for (const label of ["Имя", "Ваше имя", "Один жанр", "Быстро", "Прочтите", "Count"]) expect(text).toContain(label);
        expect(button(view.container, "Пройти проверку")).toBeTruthy();
    });

    it("reports edits with the field's key and a typed value", async () => {
        view = await render("en");
        await act(async () => change(view.container.querySelector('input[type="text"]') as HTMLInputElement, "Bob"));
        expect(onChange).toHaveBeenLastCalledWith("name", "Bob");
        const count = view.container.querySelector('input[type="number"]') as HTMLInputElement;
        await act(async () => change(count, ""));
        expect(onChange).toHaveBeenCalledTimes(1); // an empty number is not a value
        await act(async () => change(count, "7"));
        expect(onChange).toHaveBeenLastCalledWith("count", 7);
        await act(async () => (view.container.querySelector('[role="switch"]') as HTMLButtonElement).click());
        expect(onChange).toHaveBeenLastCalledWith("single", false);
        const select = view.container.querySelector("select")!;
        await act(async () => {
            select.value = "1";
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        expect(onChange).toHaveBeenLastCalledWith("mode", 1);
    });

    it("runs a button's action", async () => {
        view = await render("en");
        await act(async () => button(view.container, "Pass the check").click());
        expect(onAction).toHaveBeenCalledWith("check");
    });

    it("names the guard fields from the app's own copy", async () => {
        const guard = [field({ key: "$guardLimit", type: "number", default: 50 }), field({ key: "$guardWait", type: "select", default: 30, options: [{ value: 15 }, { value: 30 }] as plugin.Option[] })];
        setLanguage("ru");
        view = await render("ru", guard, { $guardLimit: 50, $guardWait: 30 });
        expect(view.container.textContent).toContain("Лимит треков (0 — без лимита)");
        expect(view.container.textContent).toContain("Пауза");
        expect([...view.container.querySelector("select")!.options].map((o) => o.textContent)).toEqual(["15 мин", "30 мин"]);
    });
});
