import { beforeEach, expect, it, vi } from "vitest";

const sonner = vi.hoisted(() => ({ error: vi.fn(), warning: vi.fn(), success: vi.fn(), info: vi.fn() }));
vi.mock("sonner", () => ({ toast: Object.assign(vi.fn(), sonner) }));
vi.mock("./audio", () => ({ playSuccessSound: vi.fn(), playErrorSound: vi.fn(), playWarningSound: vi.fn(), playInfoSound: vi.fn() }));
vi.mock("./logger", () => ({ logger: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() } }));
vi.mock("./settings", () => ({ getSettings: () => ({ sfxEnabled: false }) }));

import { toastWithSound } from "./toast-with-sound";

beforeEach(() => vi.clearAllMocks());

it("keeps errors and warnings on screen for 6 s, unless the caller overrides", () => {
    toastWithSound.error("Boom");
    toastWithSound.warning("Careful");
    expect(sonner.error).toHaveBeenCalledWith("boom", expect.objectContaining({ duration: 6000 }));
    expect(sonner.warning).toHaveBeenCalledWith("careful", expect.objectContaining({ duration: 6000 }));
    toastWithSound.error("Quick", { duration: 100 });
    expect(sonner.error).toHaveBeenLastCalledWith("quick", expect.objectContaining({ duration: 100 }));
});

it("leaves success and info on the global duration", () => {
    toastWithSound.success("ok");
    toastWithSound.info("fyi");
    expect(sonner.success.mock.calls[0][1]).not.toHaveProperty("duration");
    expect(sonner.info.mock.calls[0][1]).not.toHaveProperty("duration");
});
