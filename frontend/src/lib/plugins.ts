import { t } from "@/lib/i18n";
import { errorMessage } from "@/lib/utils";

/** A plugin's text in lang, else English, else its first language by code: Go's Text.In. */
export function textIn(text: Record<string, string> | null | undefined, lang: string): string {
    if (!text) return "";
    return text[lang] ?? text.en ?? text[Object.keys(text).sort()[0]] ?? "";
}

/** The ids of an import's "conflict:<id,id>" answer, null for any other error. */
export function parseConflict(err: unknown): string[] | null {
    const m = /^conflict:(.*)$/s.exec(errorMessage(err, ""));
    return m ? m[1].split(",") : null;
}

/** An import that failed part-way answers "installed:<id,id>:<error>"; null for any other error. */
export function parsePartial(err: unknown): { ids: string[]; error: string } | null {
    const m = /^installed:([^:]*):(.*)$/s.exec(errorMessage(err, ""));
    return m ? { ids: m[1].split(","), error: m[2] } : null;
}

/** Go's "no_builtin_plugins:<dir>" (an exe-only upgrade left no plugins folder) as text for the user; any other error as it is. */
export function explainMissingPlugins(err: unknown): unknown {
    const m = /^no_builtin_plugins:(.*)$/s.exec(errorMessage(err, ""));
    return m ? t("plugins.builtinMissing", { dir: m[1] }) : err;
}

/** A copy of list with the item at from moved to to. */
export function move<T>(list: readonly T[], from: number, to: number): T[] {
    const out = [...list];
    out.splice(to, 0, ...out.splice(from, 1));
    return out;
}
