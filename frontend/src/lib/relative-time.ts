import { getLanguage } from "./i18n";

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 365 * 86400], ["month", 30 * 86400], ["week", 7 * 86400], ["day", 86400], ["hour", 3600], ["minute", 60],
];

// "2 years ago" / "2 года назад" in the current language; under a minute (or in the future) reads "now".
export function formatRelativeTime(date: Date | string | number): string {
    const seconds = Math.floor((Date.now() - new Date(date).getTime()) / 1000);
    const formatter = new Intl.RelativeTimeFormat(getLanguage(), { numeric: "auto" });
    const [unit, size] = UNITS.find(([, size]) => seconds >= size) ?? ["second", 1];
    return formatter.format(seconds < 60 ? 0 : -Math.floor(seconds / size), unit);
}
