import type { SourceInfo } from "@/hooks/useSources";

export const SPOTIFY: SourceInfo = { service: "spotify", label: "Spotify", icon: "", can: ["metadata", "search", "searchByType", "isrc", "preview", "parseLink"] };
export const AMAZON: SourceInfo = { service: "amazon", label: "Amazon Music", icon: "", can: ["metadata", "search", "find", "tracks", "album", "preview", "parseLink"] };

/** A source that cannot preview. */
export const MUTE: SourceInfo = { service: "mute", label: "Mute", icon: "", can: ["metadata"] };

let current: SourceInfo[] = [SPOTIFY, AMAZON, MUTE];
/** What the mocked useSources returns; a test sets it and the next render sees it. */
export const setSources = (list: SourceInfo[]) => { current = list; };

/** The module a test swaps in for "@/hooks/useSources": vi.mock("@/hooks/useSources", async () => (await import("../test/sources")).useSourcesMock). */
export const useSourcesMock = {
    useSources: () => {
        const sources = current;
        return {
            sources,
            label: (service: string) => sources.find((s) => s.service === service)?.label ?? service,
            can: (service: string, what: string) => !!sources.find((s) => s.service === service)?.can.includes(what),
        };
    },
    linkLabels: (sources: SourceInfo[]) => sources.filter((s) => s.can.includes("parseLink")).map((s) => s.label).join(" / "),
};
