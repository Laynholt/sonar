import { describe, expect, it, vi } from "vitest";
import { closeFFmpegInstall, getFFmpegInstall, openFFmpegInstall, subscribeFFmpegInstall } from "./ffmpeg-install";

describe("ffmpeg install store", () => {
    it("opens with the callback, notifies, and closes", () => {
        const listener = vi.fn(), done = vi.fn();
        const off = subscribeFFmpegInstall(listener);
        openFFmpegInstall(done);
        expect(getFFmpegInstall()?.onInstalled).toBe(done);
        closeFFmpegInstall();
        expect(getFFmpegInstall()).toBeNull();
        expect(listener).toHaveBeenCalledTimes(2);
        off();
        openFFmpegInstall();
        expect(listener).toHaveBeenCalledTimes(2);
        closeFFmpegInstall();
    });
});
