// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { GetPreviewURL } from "../../wailsjs/go/main/App";
import { stopPreview, usePreview } from "./usePreview";
import { mount, deferred } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

const played: string[] = [];
const audios: HTMLAudioElement[] = [];
const listeners: Record<string, () => void> = {};
let api: ReturnType<typeof usePreview>;
function Probe() { api = usePreview(); return null; }
let other: ReturnType<typeof usePreview>;
function Other() { other = usePreview(); return null; }
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    played.length = 0;
    audios.length = 0;
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("Audio", class {
        pauses = 0;
        constructor(public src: string) { audios.push(this as unknown as HTMLAudioElement); }
        addEventListener(name: string, fn: () => void) { listeners[name] = fn; }
        pause() { this.pauses++; }
        currentTime = 0;
        play() { played.push(this.src); return Promise.resolve(); }
    });
    view = await mount(<><Probe /><Other /></>);
});
afterEach(async () => { stopPreview(); await view.unmount(); vi.unstubAllGlobals(); });

it("drops a preview URL that resolves after a newer play request", async () => {
    const a = deferred<string>();
    const b = deferred<string>();
    vi.mocked(GetPreviewURL).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise);
    let first!: Promise<void>, second!: Promise<void>;
    await act(async () => { first = api.playPreview("spotify", "A", "A"); });
    await act(async () => { second = api.playPreview("spotify", "B", "B"); });
    await act(async () => { b.resolve("urlB"); await second; });
    await act(async () => { a.resolve("urlA"); await first; });
    expect(played).toEqual(["urlB"]);
});

it("asks the track's own source for the preview", async () => {
    vi.mocked(GetPreviewURL).mockResolvedValueOnce("url1");
    await act(async () => { await api.playPreview("amazon", "B0X", "T"); });
    expect(GetPreviewURL).toHaveBeenCalledWith("amazon", "B0X");
});

it("drops a pending preview when stopped", async () => {
    const a = deferred<string>();
    vi.mocked(GetPreviewURL).mockReturnValueOnce(a.promise);
    let first!: Promise<void>;
    await act(async () => { first = api.playPreview("spotify", "A", "A"); });
    await act(async () => { api.stopPreview(); });
    await act(async () => { a.resolve("urlA"); await first; });
    expect(played).toEqual([]);
    expect(api.loadingPreview).toBeNull();
});

it("shares one preview across components and stops the previous track", async () => {
    vi.mocked(GetPreviewURL).mockResolvedValueOnce("url1").mockResolvedValueOnce("url2");
    await act(async () => { await api.playPreview("spotify", "t1", "T1"); });
    await act(async () => listeners.loadeddata());
    await act(async () => { await other.playPreview("spotify", "t2", "T2"); });
    await act(async () => listeners.loadeddata());

    expect((audios[0] as unknown as { pauses: number }).pauses).toBe(1);
    expect(api.playingTrack).toBe("t2");
    expect(other.playingTrack).toBe("t2");

    await act(async () => stopPreview());
    expect(api.playingTrack).toBeNull();
    expect(other.playingTrack).toBeNull();
});
