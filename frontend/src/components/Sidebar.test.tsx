// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { Sidebar } from "./Sidebar";
import { TooltipProvider } from "./ui/tooltip";
import { mount } from "../test/render";

vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
let view: Awaited<ReturnType<typeof mount>>;
const show = async (activeCount: number) => {
    view = await mount(<TooltipProvider><Sidebar currentPage="main" onPageChange={vi.fn()} activeCount={activeCount}/></TooltipProvider>);
    return view.container.querySelector<HTMLElement>('[aria-label^="Downloads"]')!;
};
afterEach(async () => { await view.unmount(); });

describe("Sidebar", () => {
    it("shows the number of active downloads on the Downloads button", async () => {
        const downloads = await show(3);
        expect(downloads.textContent).toBe("3");
        expect(downloads.getAttribute("aria-label")).toBe("Downloads (3)");
    });

    it("shows no badge when nothing is active", async () => {
        const downloads = await show(0);
        expect(downloads.textContent).toBe("");
        expect(downloads.getAttribute("aria-label")).toBe("Downloads");
    });
});
