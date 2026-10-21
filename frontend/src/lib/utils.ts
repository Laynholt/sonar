import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";

export function cn(...inputs: ClassValue[]) {
    return twMerge(clsx(inputs));
}

export function openExternal(url: string) {
    if (!url)
        return;
    try {
        BrowserOpenURL(url);
    }
    catch {
        if (typeof window !== "undefined") {
            window.open(url, "_blank", "noopener,noreferrer");
        }
    }
}
