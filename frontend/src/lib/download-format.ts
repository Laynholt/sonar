const MB = 1024 * 1024;

/** Megabytes with one decimal, in the UI language. */
export const mb = (bytes: number, language: string) =>
    (bytes / MB).toLocaleString(language, { maximumFractionDigits: 1, minimumFractionDigits: 1 });

/** Milliseconds as "m:ss", rounded up. */
export const clock = (ms: number) => {
    const s = Math.ceil(Math.max(0, ms) / 1000);
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};

/** Time left at the given speed as "m:ss", or "" when the speed or size is unknown. */
export const eta = (left: number, speed: number) => (left <= 0 || speed <= 0 ? "" : clock(Math.ceil(left / speed) * 1000));

const SITE_NAMES: Record<string, string> = { lucida: "Lucida", doubledouble: "DoubleDouble" };
/** A download site's display name; an unknown site keeps the name the backend gave it. */
export const siteName = (name: string) => SITE_NAMES[name] ?? name;
