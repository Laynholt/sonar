// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { history } from "../../wailsjs/go/models";
import { FetchHistory } from "./FetchHistory";
import { mount } from "../test/render";

const items = ["a", "b", "c"].map((id) => ({ id, type: "album", name: `Album ${id}`, image: "" })) as unknown as history.FetchItem[];
let view: Awaited<ReturnType<typeof mount>>;
const size = (prop: "scrollWidth" | "clientWidth", value: number) =>
    Object.defineProperty(HTMLElement.prototype, prop, { configurable: true, get: () => value });

beforeEach(() => vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true));
afterEach(async () => {
    await view.unmount();
    vi.unstubAllGlobals();
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollWidth;
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).clientWidth;
});

describe("FetchHistory", () => {
    it("shows no arrows while the cards fit", async () => {
        view = await mount(<FetchHistory history={items} onSelect={vi.fn()} onRemove={vi.fn()}/>);
        expect(view.container.querySelectorAll("[aria-label='Scroll on'], [aria-label='Scroll back']").length).toBe(0);
        expect(view.container.textContent).toContain("Album c");
    });

    it("pages with the arrows and wraps at the ends", async () => {
        size("scrollWidth", 1000);
        size("clientWidth", 300);
        view = await mount(<FetchHistory history={items} onSelect={vi.fn()} onRemove={vi.fn()}/>);
        const row = view.container.querySelector<HTMLElement>(".overflow-x-auto")!;
        const scrollTo = vi.fn();
        row.scrollTo = scrollTo as unknown as typeof row.scrollTo;
        let left = 0;
        Object.defineProperty(row, "scrollLeft", { configurable: true, get: () => left, set: (v) => { left = v; } });
        const press = (label: string) => act(async () => view.container.querySelector<HTMLElement>(`[aria-label="${label}"]`)!.click());

        await press("Scroll on");
        expect(scrollTo).toHaveBeenLastCalledWith({ left: 240, behavior: "smooth" });
        await press("Scroll back");
        expect(scrollTo).toHaveBeenLastCalledWith({ left: 700, behavior: "smooth" });
        left = 700;
        await press("Scroll on");
        expect(scrollTo).toHaveBeenLastCalledWith({ left: 0, behavior: "smooth" });

        // The wheel scrolls the row sideways.
        await act(async () => row.dispatchEvent(new WheelEvent("wheel", { deltaY: 100, cancelable: true })));
        expect(left).toBe(800);
    });
});
