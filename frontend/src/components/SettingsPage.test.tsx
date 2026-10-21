// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getLanguage, setLanguage } from "@/lib/i18n";
import { SettingsPage } from "./SettingsPage";
import { mount, button } from "../test/render";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));
vi.mock("../../wailsjs/go/main/App", () => ({ LoadSettings: vi.fn(), SaveSettings: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const onUnsavedChangesChange = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
    view = await mount(<SettingsPage onUnsavedChangesChange={onUnsavedChangesChange}/>);
});
afterEach(async () => { await view.unmount(); setLanguage("en"); vi.unstubAllGlobals(); });

describe("SettingsPage", () => {
    it("shows a section per nav button, starting with General", async () => {
        expect(["General", "Appearance", "Network"].map((label) => button(view.container, label).textContent)).toEqual(["General", "Appearance", "Network"]);
        expect(view.container.textContent).toContain("Language");
        expect(view.container.textContent).toContain("Sound Effects");
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).toBeNull();

        await act(async () => button(view.container, "Network").click());
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).not.toBeNull();
        expect(view.container.textContent).not.toContain("Sound Effects");
    });

    it("switching sections alone does not mark the page unsaved", async () => {
        await act(async () => button(view.container, "Appearance").click());
        await act(async () => button(view.container, "Network").click());
        expect(onUnsavedChangesChange).not.toHaveBeenCalledWith(true);
    });

    it("previews a picked language at once and marks the page unsaved", async () => {
        const select = view.container.querySelector("select")!;
        expect([...select.options].map((option) => option.value)).toEqual(["auto", "en", "ru"]);
        await act(async () => {
            select.value = "ru";
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        expect(getLanguage()).toBe("ru");
        expect(onUnsavedChangesChange).toHaveBeenLastCalledWith(true);
    });
});
