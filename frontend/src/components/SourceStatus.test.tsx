// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import type { DownloadItem } from "@/lib/downloads";
import { GetSourceStatus } from "../../wailsjs/go/main/App";
import { SourceStatus } from "./SourceStatus";
import { mount } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ GetSourceStatus: vi.fn(), PassDoubleDoubleCheck: vi.fn() }));
vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));

const item = (id: string, status: string, message = "") => ({ id, status, message }) as DownloadItem;
const store = (items: DownloadItem[]) =>
    vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount: 0, guard: {} } as unknown as ReturnType<typeof useDownloads>);
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(GetSourceStatus).mockResolvedValue([]);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("SourceStatus", () => {
    it("reloads when a download changes status, not on every poll", async () => {
        store([item("a", "downloading")]);
        view = await mount(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(1);
        store([item("a", "downloading", "50%")]); // a poll with only a new message
        await view.render(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(1);
        store([item("a", "failed")]);
        await view.render(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(2);
    });
});
