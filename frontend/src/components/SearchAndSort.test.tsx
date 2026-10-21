// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { mount } from "../test/render";
import { SearchAndSort } from "./SearchAndSort";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));

const options = async (hasPlays: boolean) => {
    const view = await mount(<SearchAndSort searchQuery="" sortBy="default" hasPlays={hasPlays} onSearchChange={vi.fn()} onSortChange={vi.fn()}/>);
    const values = [...view.container.querySelectorAll("option")].map((o) => o.value);
    await view.unmount();
    return values;
};

describe("SearchAndSort", () => {
    it("offers the plays sorts only when the list has plays", async () => {
        expect(await options(true)).toEqual(expect.arrayContaining(["plays-asc", "plays-desc"]));
        expect(await options(false)).not.toContain("plays-asc");
        expect(await options(false)).not.toContain("plays-desc");
    });
});
