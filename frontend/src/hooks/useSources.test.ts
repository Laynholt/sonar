// @vitest-environment jsdom
import { act, createElement } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ListSources } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { mount } from "../test/render";
import { AMAZON, SPOTIFY } from "../test/sources";
import { linkLabels, useSources } from "./useSources";

vi.mock("../../wailsjs/go/main/App", () => ({ ListSources: vi.fn() }));
vi.mock("../../wailsjs/runtime/runtime", () => ({ EventsOn: vi.fn() }));

let api!: ReturnType<typeof useSources>;
function Probe() { api = useSources(); return null; }
let changed: () => void;
const off = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(ListSources).mockResolvedValue([SPOTIFY, AMAZON]);
    vi.mocked(EventsOn).mockImplementation((_name, callback) => { changed = callback; return off; });
    view = await mount(createElement(Probe));
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

it("loads the sources once", () => {
    expect(ListSources).toHaveBeenCalledTimes(1);
    expect(EventsOn).toHaveBeenCalledWith("plugins:changed", expect.any(Function));
    expect(api.sources.map((s) => s.service)).toEqual(["spotify", "amazon"]);
});

it("reloads on plugins:changed and shows the new list", async () => {
    vi.mocked(ListSources).mockResolvedValue([SPOTIFY]);
    await act(async () => changed());
    expect(ListSources).toHaveBeenCalledTimes(2);
    expect(api.sources.map((s) => s.service)).toEqual(["spotify"]);
});

it("names a source by its label and an unknown one by its id", () => {
    expect(api.label("amazon")).toBe("Amazon Music");
    expect(api.label("nowhere")).toBe("nowhere");
});

it("says what a source can do; an unknown source can do nothing", () => {
    expect(api.can("spotify", "searchByType")).toBe(true);
    expect(api.can("amazon", "searchByType")).toBe(false);
    expect(api.can("nowhere", "search")).toBe(false);
});

it("keeps the last list and logs when a reload fails", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(ListSources).mockRejectedValue(new Error("down"));
    await act(async () => changed());
    expect(api.sources.map((s) => s.service)).toEqual(["spotify", "amazon"]);
    expect(console.error).toHaveBeenCalled();
});

it("stops listening on unmount", async () => {
    await view.unmount();
    expect(off).toHaveBeenCalledTimes(1);
    view = await mount(createElement(Probe));
});

it("joins the labels of the sources that take links", () => {
    expect(linkLabels([SPOTIFY, AMAZON, { ...AMAZON, label: "Mute", can: [] }])).toBe("Spotify / Amazon Music");
});
