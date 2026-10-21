// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GetPreviewURL } from "../../wailsjs/go/main/App";
import { usePreview } from "./usePreview";
import { mount, deferred } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

const played: string[] = [];
let api: ReturnType<typeof usePreview>;
function Probe() { api = usePreview(); return null; }
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    played.length = 0;
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("Audio", class {
        constructor(public src: string) {}
        addEventListener() {}
        pause() {}
        currentTime = 0;
        play() { played.push(this.src); return Promise.resolve(); }
    });
    view = await mount(<Probe />);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

it("drops a preview URL that resolves after a newer play request", async () => {
    const a = deferred<string>();
    const b = deferred<string>();
    vi.mocked(GetPreviewURL).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    let first!: Promise<void>, second!: Promise<void>;
    await act(async () => { first = api.playPreview("A", "A"); });
    await act(async () => { second = api.playPreview("B", "B"); });
    await act(async () => { b.resolve("urlB"); await second; });
    await act(async () => { a.resolve("urlA"); await first; });
    expect(played).toEqual(["urlB"]);
});

it("drops a pending preview when stopped", async () => {
    const a = deferred<string>();
    vi.mocked(GetPreviewURL).mockReturnValueOnce(a.promise);
    let first!: Promise<void>;
    await act(async () => { first = api.playPreview("A", "A"); });
    await act(async () => { api.stopPreview(); });
    await act(async () => { a.resolve("urlA"); await first; });
    expect(played).toEqual([]);
    expect(api.loadingPreview).toBeNull();
});
