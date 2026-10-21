import type { LoadedMetadata } from "@/types/api";

// What the list showed when the user clicked on, so Back can restore it.
export type View = { searchQuery: string; sortBy: string; page: number; tab?: string };
export type NavEntry = LoadedMetadata & { view?: View };
export type NavState = { current: NavEntry | null; back: NavEntry[] };
export type NavAction =
    | { type: "open"; entry: LoadedMetadata }
    | { type: "push"; entry: LoadedMetadata; view: View }
    | { type: "back" }
    | { type: "close" };

export const emptyNav: NavState = { current: null, back: [] };

export function navigate(state: NavState, action: NavAction): NavState {
    switch (action.type) {
        case "open":
            return { current: action.entry, back: [] };
        case "push":
            return {
                current: action.entry,
                back: state.current ? [...state.back, { ...state.current, view: action.view }] : state.back,
            };
        case "back":
            return state.back.length ? { current: state.back[state.back.length - 1], back: state.back.slice(0, -1) } : state;
        case "close":
            return emptyNav;
    }
}
