// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "@/lib/i18n";
import { toastWithSound } from "@/lib/toast-with-sound";
import type { main, plugin } from "../../wailsjs/go/models";
import {
    ExportPlugins, ImportPlugins, InstallPlugin, ListPlugins, OpenPluginsFolder, RefreshPlugins, RemovePlugin,
    RunPluginAction, SetPluginEnabled, SetPluginOrder, SetPluginSettings,
} from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { PluginsPage, SAVE_DELAY } from "./PluginsPage";
import { button, change, deferred, mount } from "../test/render";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));
vi.mock("../../wailsjs/go/main/App", () => ({
    ListPlugins: vi.fn(), RefreshPlugins: vi.fn(), SetPluginEnabled: vi.fn(), SetPluginOrder: vi.fn(), SetPluginSettings: vi.fn(),
    RunPluginAction: vi.fn(), InstallPlugin: vi.fn(), RemovePlugin: vi.fn(), ExportPlugins: vi.fn(), ImportPlugins: vi.fn(), OpenPluginsFolder: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({ EventsOn: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const plug = (p: Partial<main.PluginView>) => ({
    id: "", name: "", version: "1.0.0", author: "", description: "", kind: "source", dir: "", error: "", builtin: true, enabled: true,
    trusted: true, shadowed: [], fields: [], values: {}, dependents: [], blocked: "", ...p,
}) as main.PluginView;
const field = (f: Partial<plugin.Field>) => ({ key: "", type: "text", label: {}, hint: {}, default: null, options: [], action: "", ...f }) as plugin.Field;

const AMAZON = plug({ id: "amazon", name: "Amazon Music", author: "sonar", dependents: ["lucida"] });
const LUCIDA = plug({
    id: "lucida", name: "Lucida", kind: "downloader", version: "2.1.0", author: "sonar",
    fields: [field({ key: "token", type: "text", label: { en: "Token" } }), field({ key: "check", type: "button", label: { en: "Check" }, action: "check" }),
        field({ key: "$guardLimit", type: "number" }), field({ key: "$guardWait", type: "select", options: [{ value: 15 }, { value: 30 }] as plugin.Option[] })],
    values: { token: "x", $guardLimit: 50, $guardWait: 30 },
});
const EXTRA = plug({ id: "extra", name: "Extra", kind: "tagger", builtin: false, enabled: false, trusted: false, author: "Ann", blocked: "Spotify", shadowed: ["C:\\old\\extra (0.9.0)"] });
const BROKEN = plug({ id: "broken", name: "Broken", builtin: false, enabled: false, trusted: false, error: "plugin.json: bad id" });

let view: Awaited<ReturnType<typeof mount>>;
const body = () => document.body;
const row = (name: string) => [...view.container.querySelectorAll("[data-plugin]")].find((r) => r.getAttribute("data-plugin") === name)!;
const rowSwitch = (id: string) => row(id).querySelector('[role="switch"]') as HTMLButtonElement;
const dialog = () => document.querySelector('[role="dialog"]') as HTMLElement | null;
const open = async (list = [AMAZON, LUCIDA, EXTRA, BROKEN]) => {
    vi.mocked(ListPlugins).mockResolvedValue(list);
    view = await mount(<PluginsPage/>);
};

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(EventsOn).mockReturnValue(() => {});
    vi.mocked(RefreshPlugins).mockResolvedValue();
    vi.mocked(SetPluginEnabled).mockResolvedValue();
    vi.mocked(SetPluginOrder).mockResolvedValue();
    vi.mocked(SetPluginSettings).mockResolvedValue();
    vi.mocked(ImportPlugins).mockResolvedValue([]);
});
afterEach(async () => { await view?.unmount(); setLanguage("en"); vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("PluginsPage list", () => {
    it("refreshes once on open and shows each plugin", async () => {
        await open();
        expect(RefreshPlugins).toHaveBeenCalledTimes(1);
        const text = row("lucida").textContent!;
        for (const part of ["Lucida", "Downloader", "sonar", "2.1.0", "Built-in"]) expect(text).toContain(part);
        expect(rowSwitch("lucida").getAttribute("aria-checked")).toBe("true");
        expect(row("extra").textContent).toContain("Third-party");
        expect(row("extra").textContent).toContain("Ann");
    });

    it("offers Remove on third-party rows only", async () => {
        await open();
        expect(() => button(row("lucida"), "Remove")).toThrow();
        expect(() => button(row("amazon"), "Remove")).toThrow();
        expect(button(row("extra"), "Remove")).toBeTruthy();
    });

    it("shows blocked, shadowed and error, and a broken plugin cannot be switched on", async () => {
        await open();
        expect(row("extra").textContent).toContain("needs source Spotify");
        expect(row("extra").textContent).toContain("C:\\old\\extra (0.9.0) replaced by version 1.0.0");
        expect(row("broken").textContent).toContain("plugin.json: bad id");
        expect(rowSwitch("broken").disabled).toBe(true);
        expect(rowSwitch("extra").disabled).toBe(false);
    });

    it("shows its own copy in Russian", async () => {
        setLanguage("ru");
        await open();
        expect(row("extra").textContent).toContain("нужен источник Spotify");
        expect(row("extra").textContent).toContain("заменён версией 1.0.0");
        expect(row("extra").textContent).toContain("Сторонний");
        expect(row("lucida").textContent).toContain("Встроенный");
        for (const label of ["Обновить список", "Установить из файла…", "Экспортировать все…", "Импортировать…", "Открыть папку плагинов"]) expect(button(view.container, label)).toBeTruthy();
    });
});

describe("PluginsPage switches", () => {
    it("asks for trust when a third-party plugin needs it, then enables it trusted", async () => {
        await open();
        vi.mocked(SetPluginEnabled).mockRejectedValueOnce("needs_trust");
        await act(async () => rowSwitch("extra").click());
        expect(SetPluginEnabled).toHaveBeenLastCalledWith("extra", true, false);
        expect(dialog()!.textContent).toContain("A plugin can do anything on your computer. Only turn on plugins you trust.");
        await act(async () => button(dialog()!, "Turn on").click());
        expect(SetPluginEnabled).toHaveBeenLastCalledWith("extra", true, true);
    });

    it("lists a source's dependents before turning it off", async () => {
        await open();
        await act(async () => rowSwitch("amazon").click());
        expect(SetPluginEnabled).not.toHaveBeenCalled();
        expect(dialog()!.textContent).toContain("Lucida");
        await act(async () => button(dialog()!, "Turn off").click());
        expect(SetPluginEnabled).toHaveBeenCalledWith("amazon", false, false);
    });

    it("turns off a plugin nothing needs at once", async () => {
        await open();
        await act(async () => rowSwitch("lucida").click());
        expect(SetPluginEnabled).toHaveBeenCalledWith("lucida", false, false);
        expect(dialog()).toBeNull();
    });

    it("removes a third-party plugin after asking", async () => {
        await open();
        vi.mocked(RemovePlugin).mockResolvedValue();
        await act(async () => button(row("extra"), "Remove").click());
        await act(async () => button(dialog()!, "Remove").click());
        expect(RemovePlugin).toHaveBeenCalledWith("extra");
    });
});

describe("PluginsPage order", () => {
    beforeEach(() => { // the backend lists plugins in the order last saved
        vi.mocked(SetPluginOrder).mockImplementation(async (ids) => {
            const list = await ListPlugins();
            vi.mocked(ListPlugins).mockResolvedValue(ids.map((id) => list.find((p) => p.id === id)!));
        });
    });

    it("moves a plugin with the up and down buttons", async () => {
        await open();
        await act(async () => (row("lucida").querySelector('[aria-label="Move up"]') as HTMLButtonElement).click());
        expect(SetPluginOrder).toHaveBeenLastCalledWith(["lucida", "amazon", "extra", "broken"]);
        await act(async () => (row("amazon").querySelector('[aria-label="Move down"]') as HTMLButtonElement).click());
        expect(SetPluginOrder).toHaveBeenLastCalledWith(["lucida", "extra", "amazon", "broken"]);
    });

    it("moves a plugin by dragging its grip", async () => {
        await open();
        await act(async () => {
            row("broken").querySelector("[data-drag-handle]")!.dispatchEvent(new Event("dragstart", { bubbles: true }));
            row("amazon").dispatchEvent(new Event("drop", { bubbles: true }));
        });
        expect(SetPluginOrder).toHaveBeenLastCalledWith(["broken", "amazon", "lucida", "extra"]);
    });

    it("does not reorder on text dragged out of a settings field, or after a drag ended", async () => {
        await open();
        await act(async () => {
            row("lucida").querySelector('input[type="text"]')!.dispatchEvent(new Event("dragstart", { bubbles: true }));
            row("amazon").dispatchEvent(new Event("drop", { bubbles: true }));
            const grip = row("broken").querySelector("[data-drag-handle]")!;
            grip.dispatchEvent(new Event("dragstart", { bubbles: true }));
            grip.dispatchEvent(new Event("dragend", { bubbles: true }));
            row("amazon").dispatchEvent(new Event("drop", { bubbles: true }));
        });
        expect(SetPluginOrder).not.toHaveBeenCalled();
    });
});

describe("PluginsPage settings", () => {
    it("saves an edit with the merged values after a pause", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
        await open();
        const token = row("lucida").querySelector('input[type="text"]') as HTMLInputElement;
        await act(async () => change(token, "y"));
        await act(async () => change(token, "yz"));
        expect(SetPluginSettings).not.toHaveBeenCalled();
        await act(async () => { await vi.advanceTimersByTimeAsync(SAVE_DELAY); });
        expect(SetPluginSettings).toHaveBeenCalledTimes(1);
        expect(SetPluginSettings).toHaveBeenCalledWith("lucida", { token: "yz", $guardLimit: 50, $guardWait: 30 });
    });

    it("saves the guard fields with the other values", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
        await open();
        await act(async () => change(row("lucida").querySelector('input[type="number"]') as HTMLInputElement, "0"));
        const wait = row("lucida").querySelector("select")!;
        await act(async () => {
            wait.value = "15";
            wait.dispatchEvent(new Event("change", { bubbles: true }));
        });
        await act(async () => { await vi.advanceTimersByTimeAsync(SAVE_DELAY); });
        expect(SetPluginSettings).toHaveBeenCalledWith("lucida", { token: "x", $guardLimit: 0, $guardWait: 15 });
    });

    it("saves a pending edit when the page closes", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
        await open();
        await act(async () => change(row("lucida").querySelector('input[type="text"]') as HTMLInputElement, "q"));
        await view.unmount();
        expect(SetPluginSettings).toHaveBeenCalledWith("lucida", { token: "q", $guardLimit: 50, $guardWait: 30 });
    });

    it("runs a button's action", async () => {
        await open();
        vi.mocked(RunPluginAction).mockResolvedValue();
        await act(async () => button(row("lucida"), "Check").click());
        expect(RunPluginAction).toHaveBeenCalledWith("lucida", "check");
    });

    it("keeps the buttons waiting with a spinner while the action runs", async () => {
        await open();
        const done = deferred<void>();
        vi.mocked(RunPluginAction).mockReturnValue(done.promise);
        await act(async () => button(row("lucida"), "Check").click());
        expect(button(row("lucida"), "Check").disabled).toBe(true);
        expect(button(row("lucida"), "Check").querySelector('[role="status"]')).toBeTruthy();
        expect(rowSwitch("amazon").disabled).toBe(false); // other plugins are not held up
        await act(async () => done.resolve());
        expect(button(row("lucida"), "Check").disabled).toBe(false);
        expect(button(row("lucida"), "Check").querySelector('[role="status"]')).toBeNull();
    });

    it("keeps an edit made while the last one was saving", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
        await open();
        const saving = deferred<void>();
        vi.mocked(SetPluginSettings).mockReturnValueOnce(saving.promise);
        const token = () => row("lucida").querySelector('input[type="text"]') as HTMLInputElement;
        await act(async () => change(token(), "y"));
        await act(async () => { await vi.advanceTimersByTimeAsync(SAVE_DELAY); });
        await act(async () => change(token(), "yz"));
        vi.mocked(ListPlugins).mockResolvedValue([AMAZON, plug({ ...LUCIDA, values: { ...LUCIDA.values, token: "y" } }), EXTRA, BROKEN]);
        await act(async () => saving.resolve());
        expect(token().value).toBe("yz");
        await act(async () => { await vi.advanceTimersByTimeAsync(SAVE_DELAY); });
        expect(SetPluginSettings).toHaveBeenLastCalledWith("lucida", { token: "yz", $guardLimit: 50, $guardWait: 30 });
    });

    it("shows the saved values again when a save fails", async () => {
        vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
        await open();
        vi.mocked(SetPluginSettings).mockRejectedValueOnce("disk full");
        const limit = row("lucida").querySelector('input[type="number"]') as HTMLInputElement;
        await act(async () => change(row("lucida").querySelector('input[type="text"]') as HTMLInputElement, "y"));
        await act(async () => change(limit, "7"));
        await act(async () => { await vi.advanceTimersByTimeAsync(SAVE_DELAY); });
        expect(toastWithSound.error).toHaveBeenCalledWith("disk full");
        expect((row("lucida").querySelector('input[type="text"]') as HTMLInputElement).value).toBe("x");
        expect(limit.value).toBe("50");
    });
});

describe("PluginsPage top buttons", () => {
    it("refreshes, exports and opens the folder", async () => {
        await open();
        vi.mocked(ExportPlugins).mockResolvedValue("C:\\out.zip");
        vi.mocked(OpenPluginsFolder).mockResolvedValue();
        await act(async () => button(view.container, "Refresh list").click());
        expect(RefreshPlugins).toHaveBeenCalledTimes(2);
        await act(async () => button(view.container, "Export all…").click());
        expect(ExportPlugins).toHaveBeenCalled();
        await act(async () => button(view.container, "Open plugins folder").click());
        expect(OpenPluginsFolder).toHaveBeenCalled();
    });

    it("asks before replacing an installed plugin", async () => {
        await open();
        vi.mocked(InstallPlugin).mockRejectedValueOnce("exists").mockResolvedValueOnce("extra");
        await act(async () => button(view.container, "Install from file…").click());
        expect(InstallPlugin).toHaveBeenLastCalledWith(false);
        expect(dialog()!.textContent).toContain("Replace?");
        await act(async () => button(dialog()!, "Replace").click());
        expect(InstallPlugin).toHaveBeenLastCalledWith(true);
    });

    it("on an import conflict asks once, then one trust dialog for the returned ids", async () => {
        await open();
        vi.mocked(ImportPlugins).mockRejectedValueOnce("conflict:extra,broken").mockResolvedValueOnce(["extra", "broken"]);
        await act(async () => button(view.container, "Import…").click());
        expect(ImportPlugins).toHaveBeenLastCalledWith("");
        expect(dialog()!.textContent).toContain("Extra");
        expect(button(dialog()!, "Replace all")).toBeTruthy();
        await act(async () => button(dialog()!, "Skip matching").click());
        expect(ImportPlugins).toHaveBeenLastCalledWith("skip");
        expect(dialog()!.textContent).toContain("A plugin can do anything on your computer.");
        expect(dialog()!.textContent).toContain("Extra, Broken");
        await act(async () => button(dialog()!, "Turn on").click());
        expect(SetPluginEnabled).toHaveBeenCalledWith("extra", true, true);
        expect(SetPluginEnabled).toHaveBeenCalledWith("broken", true, true);
        expect(ImportPlugins).toHaveBeenCalledTimes(2);
    });

    it("names what a failed import installed before it stopped", async () => {
        await open();
        vi.mocked(ImportPlugins).mockRejectedValueOnce('installed:extra:plugin "zz": Access is denied.');
        await act(async () => button(view.container, "Import…").click());
        expect(vi.mocked(toastWithSound.error).mock.lastCall![0]).toBe('Installed Extra, then the import stopped: plugin "zz": Access is denied.');
    });

    it("names plugins the import just added, which the shown list lacks", async () => {
        await open([AMAZON, LUCIDA]);
        const fresh = plug({ id: "fresh", name: "Fresh one", builtin: false, enabled: false, trusted: false });
        vi.mocked(ImportPlugins).mockImplementationOnce(async () => {
            vi.mocked(ListPlugins).mockResolvedValue([AMAZON, LUCIDA, fresh]);
            return ["fresh"];
        }).mockImplementationOnce(async () => {
            vi.mocked(ListPlugins).mockResolvedValue([AMAZON, LUCIDA, fresh, plug({ id: "late", name: "Late one" })]);
            throw "installed:late:boom";
        });
        await act(async () => button(view.container, "Import…").click());
        expect(dialog()!.textContent).toContain("Fresh one");
        await act(async () => button(dialog()!, "Cancel").click());
        await act(async () => button(view.container, "Import…").click());
        expect(vi.mocked(toastWithSound.error).mock.lastCall![0]).toBe("Installed Late one, then the import stopped: boom");
    });
});

describe("PluginsPage events", () => {
    it("reloads the list when a plugin changes", async () => {
        await open();
        const reload = vi.mocked(EventsOn).mock.calls.find(([name]) => name === "plugins:changed")![1];
        vi.mocked(ListPlugins).mockResolvedValue([plug({ id: "new", name: "Newcomer" })]);
        await act(async () => reload());
        expect(body().textContent).toContain("Newcomer");
    });
});
