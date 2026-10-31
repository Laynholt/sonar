import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp, GripVertical } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Switch } from "@/components/ui/switch";
import { PluginSettings } from "@/components/PluginSettings";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { move, parseConflict, parsePartial } from "@/lib/plugins";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import {
    ExportPlugins, ImportPlugins, InstallPlugin, ListPlugins, OpenPluginsFolder, RefreshPlugins, RemovePlugin,
    RunPluginAction, SetPluginEnabled, SetPluginOrder, SetPluginSettings,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime/runtime";

/** How long after the last edit a plugin's settings are saved. */
export const SAVE_DELAY = 500;

const KINDS: Record<string, MessageKey> = { source: "plugins.kindSource", downloader: "plugins.kindDownloader", tagger: "plugins.kindTagger", tool: "plugins.kindTool" };

type Values = Record<string, unknown>;
/** A question with its answers; Cancel is always offered too. */
type Ask = { title: string; body?: string; actions: { label: string; destructive?: boolean; run: () => Promise<unknown> }[] };

/** Every plugin, built-in and installed: switches, order, settings, install, export and import. */
export function PluginsPage() {
    const { t, language } = useI18n();
    const [plugins, setPlugins] = useState<main.PluginView[]>([]);
    const [drafts, setDrafts] = useState<Record<string, Values>>({}); // edits not yet in plugins' values
    const [ask, setAsk] = useState<Ask | null>(null);
    const latest = useRef(0);
    const pluginsRef = useRef(plugins);
    pluginsRef.current = plugins;
    const draftsRef = useRef(drafts);
    draftsRef.current = drafts;

    // Returns the list it fetched, or the one shown when that fails.
    const load = useCallback(async (): Promise<main.PluginView[]> => {
        const id = ++latest.current;
        try {
            const list = (await ListPlugins()) ?? [];
            if (id === latest.current) setPlugins(list);
            return list;
        }
        catch (error) {
            console.error("Failed to load the plugins:", error);
            return pluginsRef.current;
        }
    }, []);
    useEffect(() => {
        void (async () => {
            try { await RefreshPlugins(); }
            catch (error) { console.error("Failed to refresh the plugins:", error); }
            await load();
        })();
        return EventsOn("plugins:changed", () => void load());
    }, [load]);

    // Any failure becomes a toast; the list is reloaded either way.
    const run = async (action: () => Promise<unknown>) => {
        try { await action(); }
        catch (error) { toast.error(errorMessage(error, t("recent.unknownError"))); }
        finally { await load(); }
    };
    const names = (ids: string[], list = pluginsRef.current) => ids.map((id) => list.find((p) => p.id === id)?.name || id).join(", ");
    // The action each plugin is running: its buttons wait for it.
    const [running, setRunning] = useState<Record<string, string>>({});
    const act = async (id: string, action: string) => {
        setRunning((r) => ({ ...r, [id]: action }));
        try { await run(() => RunPluginAction(id, action)); }
        finally { setRunning(({ [id]: _, ...rest }) => rest); }
    };

    // Settings are saved a moment after the last edit, with the plugin's other values; leaving the page saves at once.
    const pending = useRef(new Map<string, { timer: ReturnType<typeof setTimeout>; save: () => void }>());
    useEffect(() => () => {
        for (const p of pending.current.values()) { clearTimeout(p.timer); p.save(); }
        pending.current.clear();
    }, []);
    const save = (id: string) => {
        pending.current.delete(id);
        const sent = draftsRef.current[id] ?? {};
        const values = { ...pluginsRef.current.find((p) => p.id === id)?.values, ...sent };
        // Drops the edits this save sent, saved or failed; one made since stays a draft for its own save.
        const settle = () => {
            const left = Object.entries(draftsRef.current[id] ?? {}).filter(([key, value]) => !(key in sent && Object.is(value, sent[key])));
            const { [id]: _, ...rest } = draftsRef.current;
            draftsRef.current = left.length ? { ...rest, [id]: Object.fromEntries(left) } : rest;
            setDrafts(draftsRef.current);
        };
        SetPluginSettings(id, values).then(
            async () => { await load(); settle(); }, // the saved values are in the list before the drafts go
            async (error) => {
                toast.error(errorMessage(error, t("recent.unknownError")));
                settle(); // back to what is saved
                await load();
            });
    };
    const edit = (id: string, key: string, value: unknown) => {
        draftsRef.current = { ...draftsRef.current, [id]: { ...draftsRef.current[id], [key]: value } };
        setDrafts(draftsRef.current);
        clearTimeout(pending.current.get(id)?.timer);
        pending.current.set(id, { timer: setTimeout(() => save(id), SAVE_DELAY), save: () => save(id) });
    };

    const trust = (ids: string[], list?: main.PluginView[]) => setAsk({
        title: t("plugins.trustTitle", { names: names(ids, list) }), body: t("plugins.trust"),
        actions: [{ label: t("plugins.turnOn"), run: async () => { for (const id of ids) await SetPluginEnabled(id, true, true); } }],
    });
    const toggle = (p: main.PluginView, on: boolean) => {
        if (on) {
            return run(async () => {
                try { await SetPluginEnabled(p.id, true, false); }
                catch (error) {
                    if (errorMessage(error, "") !== "needs_trust") throw error;
                    trust([p.id]);
                }
            });
        }
        if (p.dependents?.length) {
            return setAsk({ title: t("plugins.disableTitle", { name: p.name }), body: t("plugins.disableBody", { names: names(p.dependents) }),
                actions: [{ label: t("plugins.turnOff"), destructive: true, run: () => SetPluginEnabled(p.id, false, false) }] });
        }
        return run(() => SetPluginEnabled(p.id, false, false));
    };
    const remove = (p: main.PluginView) => setAsk({ title: t("plugins.removeTitle", { name: p.name }),
        actions: [{ label: t("plugins.remove"), destructive: true, run: () => RemovePlugin(p.id) }] });

    const reorder = (from: number, to: number) => {
        if (from < 0 || to < 0 || to >= plugins.length || from === to) return;
        const next = move(plugins, from, to);
        setPlugins(next);
        void run(() => SetPluginOrder(next.map((p) => p.id)));
    };
    const dragFrom = useRef(-1);

    const install = async (replace: boolean): Promise<void> => {
        try {
            const id = await InstallPlugin(replace);
            if (id) toast.success(t("plugins.installed", { id }));
        }
        catch (error) {
            if (errorMessage(error, "") !== "exists") throw error;
            setAsk({ title: t("plugins.replaceTitle"), body: t("plugins.replaceBody"), actions: [{ label: t("plugins.replace"), run: () => install(true) }] });
        }
    };
    const exportAll = async () => {
        const path = await ExportPlugins();
        if (path) toast.success(t("plugins.exported", { path }));
    };
    // The import is not undone when it fails part-way, so the error names what it installed.
    const importAll = async (choice: string): Promise<void> => {
        try {
            const ids = await ImportPlugins(choice);
            const list = await load(); // the new ones are not in the shown list yet
            if (ids?.length) trust(ids, list);
        }
        catch (error) {
            const clash = parseConflict(error);
            if (clash) {
                return setAsk({ title: t("plugins.conflictTitle", { names: names(clash) }), actions: [
                    { label: t("plugins.replaceAll"), run: () => importAll("replace") },
                    { label: t("plugins.skipExisting"), run: () => importAll("skip") },
                ] });
            }
            const partial = parsePartial(error);
            if (!partial) throw error;
            throw t("plugins.importPartial", { ids: names(partial.ids, await load()), error: partial.error });
        }
    };

    return (<div className="space-y-4">
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => void run(RefreshPlugins)}>{t("plugins.refresh")}</Button>
        <Button variant="outline" size="sm" onClick={() => void run(() => install(false))}>{t("plugins.install")}</Button>
        <Button variant="outline" size="sm" onClick={() => void run(exportAll)}>{t("plugins.export")}</Button>
        <Button variant="outline" size="sm" onClick={() => void run(() => importAll(""))}>{t("plugins.import")}</Button>
        <Button variant="outline" size="sm" onClick={() => void run(OpenPluginsFolder)}>{t("plugins.openFolder")}</Button>
      </div>

      {plugins.map((p, i) => (<section key={p.id} data-plugin={p.id} className="rounded-md border overflow-hidden"
          onDragOver={(e) => { if (dragFrom.current >= 0) e.preventDefault(); }}
          onDrop={(e) => { if (dragFrom.current < 0) return; e.preventDefault(); reorder(dragFrom.current, i); dragFrom.current = -1; }}>
        <div className="flex items-center gap-3 px-4 py-3 bg-muted/50">
          {/* Only the grip starts a move: text dragged out of a settings field must not reorder. */}
          <span draggable data-drag-handle className="cursor-grab text-muted-foreground"
              onDragStart={() => { dragFrom.current = i; }} onDragEnd={() => { dragFrom.current = -1; }}><GripVertical className="h-4 w-4"/></span>
          <div className="min-w-0 flex-1 space-y-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{p.name || p.id}</span>
              <Badge variant="secondary">{KINDS[p.kind] ? t(KINDS[p.kind]) : p.kind}</Badge>
              <Badge variant="outline">{t(p.builtin ? "plugins.builtin" : "plugins.thirdParty")}</Badge>
            </div>
            <p className="text-xs text-muted-foreground">{[p.version, p.author].filter(Boolean).join(" · ")}</p>
            {p.description && <p className="text-xs text-muted-foreground">{p.description}</p>}
            {p.error && <p className="text-xs text-destructive">{p.error}</p>}
            {p.blocked && <p className="text-xs text-muted-foreground">{t("plugins.blocked", { source: p.blocked })}</p>}
            {(p.shadowed ?? []).map((copy) => <p key={copy} className="text-xs text-muted-foreground">{t("plugins.shadowed", { copy, version: p.version })}</p>)}
          </div>
          <Button variant="ghost" size="icon-sm" aria-label={t("plugins.moveUp")} disabled={i === 0} onClick={() => reorder(i, i - 1)}><ChevronUp className="h-4 w-4"/></Button>
          <Button variant="ghost" size="icon-sm" aria-label={t("plugins.moveDown")} disabled={i === plugins.length - 1} onClick={() => reorder(i, i + 1)}><ChevronDown className="h-4 w-4"/></Button>
          {!p.builtin && <Button variant="outline" size="sm" onClick={() => remove(p)}>{t("plugins.remove")}</Button>}
          {/* A broken plugin cannot be turned on; one that is on can always be turned off, e.g. after a crash. */}
          <Switch aria-label={p.name || p.id} checked={p.enabled} disabled={!!p.error && !p.enabled} onCheckedChange={(on) => void toggle(p, on)}/>
        </div>
        {(p.fields ?? []).length > 0 && (<div className="border-t">
          <PluginSettings fields={p.fields} values={{ ...p.values, ...drafts[p.id] }} lang={language}
              onChange={(key, value) => edit(p.id, key, value)} onAction={(action) => void act(p.id, action)} running={running[p.id]}/>
        </div>)}
      </section>))}

      <Dialog open={!!ask} onOpenChange={(open) => { if (!open) setAsk(null); }}>
        {ask && <DialogContent className="sm:max-w-[425px]">
          <DialogHeader>
            <DialogTitle>{ask.title}</DialogTitle>
            {ask.body && <DialogDescription>{ask.body}</DialogDescription>}
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAsk(null)}>{t("common.cancel")}</Button>
            {ask.actions.map((a) => (<Button key={a.label} variant={a.destructive ? "destructive" : "default"} onClick={() => { setAsk(null); void run(a.run); }}>{a.label}</Button>))}
          </DialogFooter>
        </DialogContent>}
      </Dialog>
    </div>);
}
