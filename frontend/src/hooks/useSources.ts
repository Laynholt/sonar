import { useCallback, useEffect, useRef, useState } from "react";
import { ListSources } from "../../wailsjs/go/main/App";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export interface SourceInfo { service: string; label: string; icon: string; can: string[] }

/** The enabled sources, loaded when the component mounts and again whenever a plugin changes. */
export function useSources() {
    const [sources, setSources] = useState<SourceInfo[]>([]);
    const latest = useRef(0);
    useEffect(() => {
        let live = true;
        const load = async () => {
            const id = ++latest.current;
            try {
                const list = await ListSources();
                if (live && id === latest.current) setSources(list ?? []);
            }
            catch (error) { console.error("Failed to load the sources:", error); }
        };
        void load();
        const off = EventsOn("plugins:changed", () => void load());
        return () => { live = false; off(); };
    }, []);
    const label = useCallback((service: string) => sources.find((s) => s.service === service)?.label ?? service, [sources]);
    const can = useCallback((service: string, what: string) => !!sources.find((s) => s.service === service)?.can.includes(what), [sources]);
    return { sources, label, can };
}

/** The labels of the sources that take pasted links, as "A / B" for the copy that names them. */
export const linkLabels = (sources: SourceInfo[]) => sources.filter((s) => s.can.includes("parseLink")).map((s) => s.label).join(" / ");
