import { useSyncExternalStore } from "react";

/** An open FFmpeg install dialog; `onInstalled` gets the saved path once the install is done. */
export interface FFmpegInstallRequest { id: number; onInstalled?: (path: string) => void }

// The one install dialog of the app: null while closed. Every open is a new object, so a reopen restarts the dialog.
let open: FFmpegInstallRequest | null = null;
let opened = 0;
const listeners = new Set<() => void>();

function set(next: FFmpegInstallRequest | null) {
    open = next;
    listeners.forEach((listener) => listener());
}

export function subscribeFFmpegInstall(listener: () => void) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}
export const getFFmpegInstall = () => open;
export const openFFmpegInstall = (onInstalled?: (path: string) => void) => set({ id: ++opened, onInstalled });
export const closeFFmpegInstall = () => { if (open) set(null); };
export const useFFmpegInstall = () => useSyncExternalStore(subscribeFFmpegInstall, getFFmpegInstall);
