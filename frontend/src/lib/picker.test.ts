import { beforeEach, expect, it, vi } from "vitest";
import { getSnapshot, type DownloadItem } from "@/lib/downloads";
import { closePicker, getPicker, openPicker, subscribePicker } from "./picker";

vi.mock("@/lib/downloads", () => ({ getSnapshot: vi.fn() }));

const item = (id: string, status: string) => ({ id, status }) as DownloadItem;

beforeEach(() => {
    vi.resetAllMocks();
    closePicker();
    vi.mocked(getSnapshot).mockReturnValue([item("a", "completed"), item("b", "review"), item("c", "review")]);
});

it("opens the given item", () => {
    openPicker("c");
    expect(getPicker()).toBe("c");
});

it("opens the oldest review item when given none", () => {
    openPicker();
    expect(getPicker()).toBe("b");
});

it("stays closed when nothing waits for a decision", () => {
    vi.mocked(getSnapshot).mockReturnValue([item("a", "completed")]);
    openPicker();
    expect(getPicker()).toBe("");
});

it("tells subscribers about opening and closing, not about no-ops", () => {
    const seen = vi.fn();
    const off = subscribePicker(seen);
    openPicker("b");
    openPicker("b");
    closePicker();
    off();
    openPicker("c");
    expect(seen).toHaveBeenCalledTimes(2);
});
