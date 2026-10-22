import { useSyncExternalStore } from "react";
import { getSnapshot } from "@/lib/downloads";

// The one candidate picker of the app: the id of the item it is open for, "" while closed.
let open = "";
const listeners = new Set<() => void>();

function set(id: string) {
    if (id === open) return;
    open = id;
    listeners.forEach((listener) => listener());
}

export function subscribePicker(listener: () => void) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}
export const getPicker = () => open;
/** Opens the picker for an item, or for the oldest item waiting for a decision. */
export const openPicker = (itemId?: string) => set(itemId ?? getSnapshot().find((i) => i.status === "review")?.id ?? "");
export const closePicker = () => set("");
export const usePicker = () => useSyncExternalStore(subscribePicker, getPicker);
