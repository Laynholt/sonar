import { useEffect, useRef, useState } from "react";
import type { DownloadItem } from "@/lib/downloads";

// The queue snapshot only changes when something does, so a stalled download sends nothing new.
const STALL_MS = 1500;

/** Bytes per second for each downloading item, smoothed over the last few polls, and their sum. */
export function useDownloadSpeed(items: DownloadItem[]): { byId: Map<string, number>; total: number } {
    const prev = useRef({ bytes: new Map<string, number>(), at: Date.now() });
    const [byId, setById] = useState(() => new Map<string, number>());
    useEffect(() => {
        const last = prev.current;
        const now = Date.now();
        const seconds = (now - last.at) / 1000;
        const bytes = new Map(items.filter((i) => i.status === "downloading").map((i) => [i.id, i.bytes ?? 0]));
        if (seconds > 0) {
            setById((old) => new Map([...bytes].map(([id, b]) => [id, 0.5 * (old.get(id) ?? 0) + 0.5 * Math.max(0, b - (last.bytes.get(id) ?? 0)) / seconds])));
        }
        prev.current = { bytes, at: now };
        const stall = setTimeout(() => setById(new Map()), STALL_MS);
        return () => clearTimeout(stall);
    }, [items]);
    let total = 0;
    for (const v of byId.values()) total += v;
    return { byId, total };
}
