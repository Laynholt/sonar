import { useSyncExternalStore } from "react";
import { GetPreviewURL } from "@/../wailsjs/go/main/App";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import { t } from "@/lib/i18n";
// One preview for the whole app: state lives here, not in a component.
let state: {
    loadingPreview: string | null;
    playingTrack: string | null;
} = { loadingPreview: null, playingTrack: null };
let currentAudio: HTMLAudioElement | null = null;
// Bumped by every play/stop; a URL resolving after a newer request is dropped.
let requestId = 0;
const listeners = new Set<() => void>();
function setState(patch: Partial<typeof state>) {
    state = { ...state, ...patch };
    listeners.forEach((listener) => listener());
}
function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}
function haltAudio() {
    if (currentAudio) {
        currentAudio.pause();
        currentAudio.currentTime = 0;
        currentAudio = null;
    }
}
export function stopPreview() {
    requestId++;
    haltAudio();
    setState({ loadingPreview: null, playingTrack: null });
}
async function playPreview(service: string, trackId: string, trackName: string) {
    if (state.playingTrack === trackId && currentAudio) {
        stopPreview();
        return;
    }
    stopPreview();
    const request = requestId;
    try {
        setState({ loadingPreview: trackId });
        const previewURL = await GetPreviewURL(service, trackId);
        if (request !== requestId) return;
        if (!previewURL) {
            toast.error(t("preview.unavailable"), {
                description: t("preview.notFound", { track: trackName }),
            });
            setState({ loadingPreview: null });
            return;
        }
        const audio = new Audio(previewURL);
        audio.addEventListener("loadeddata", () => {
            if (audio !== currentAudio) return;
            setState({ loadingPreview: null, playingTrack: trackId });
        });
        audio.addEventListener("ended", () => {
            if (audio !== currentAudio) return;
            currentAudio = null;
            setState({ playingTrack: null });
        });
        audio.addEventListener("error", () => {
            if (audio !== currentAudio) return;
            toast.error(t("preview.playFailed"), {
                description: t("preview.cannotPlay", { track: trackName }),
            });
            currentAudio = null;
            setState({ loadingPreview: null, playingTrack: null });
        });
        currentAudio = audio;
        await audio.play();
    }
    catch (error) {
        if (request !== requestId) return;
        console.error("Preview error:", error);
        toast.error(t("preview.unavailable"), {
            description: errorMessage(error, t("preview.cannotLoad", { track: trackName })),
        });
        setState({ loadingPreview: null, playingTrack: null });
    }
}
const getState = () => state;
export function usePreview() {
    const { loadingPreview, playingTrack } = useSyncExternalStore(subscribe, getState, getState);
    return { playPreview, stopPreview, loadingPreview, playingTrack };
}
