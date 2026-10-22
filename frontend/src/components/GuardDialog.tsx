import { useEffect, useReducer, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useDownloads } from "@/hooks/useDownloads";
import { clock, siteName } from "@/lib/download-format";
import { guardContinue, guardWait } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import { getSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import type { source } from "../../wailsjs/go/models";

const DONE = ["completed", "skipped", "ignored", "not_found"];
const LEFT = ["matching", "queued", "downloading", "review"];

/** Asks what to do at the rate limit, for the first site that is asking. Controlled with no onOpenChange, so Escape and outside clicks do nothing. */
export function GuardDialog() {
    const { t } = useI18n();
    const { sites, items } = useDownloads();
    const asking = sites.find((site) => site.asking);
    const live = asking && {
        site: asking,
        others: sites.filter((site) => site !== asking),
        done: items.filter((i) => DONE.includes(i.status)).length,
        left: items.filter((i) => LEFT.includes(i.status)).length,
        minutes: getSettings().rateGuardWait,
    };
    // The exit animation keeps the dialog in the DOM after the guard stops asking; it shows the last question, not an empty box.
    const last = useRef(live);
    if (live) last.current = live;
    const shown = last.current;

    const [busy, setBusy] = useState(false);
    const answer = async (action: () => Promise<void>) => {
        setBusy(true);
        try { await action(); }
        catch (err) { toast.error(errorMessage(err, t("recent.unknownError"))); }
        finally { setBusy(false); }
    };

    // Re-render every second while another site's pause counts down.
    const [, tick] = useReducer((n: number) => n + 1, 0);
    const counting = !!live && live.others.some((site) => site.paused_until > Date.now());
    useEffect(() => {
        if (!counting) return;
        const id = setInterval(tick, 1000);
        return () => clearInterval(id);
    }, [counting]);

    const otherLine = (site: source.Status) => {
        const now = Date.now();
        const params = { site: siteName(site.name), count: site.count, limit: site.limit, time: clock(site.paused_until - now) };
        if (site.asking) return t("download.guardOtherAsking", params);
        if (site.paused_until > now) return t("download.guardOtherPaused", params);
        return t(site.available ? "download.guardOtherBusy" : "download.guardOtherDown", params);
    };

    return (<Dialog open={!!asking}>
      {shown && <DialogContent showCloseButton={false} className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{t("download.guardTitle", { site: siteName(shown.site.name), count: shown.site.count })}</DialogTitle>
          <DialogDescription>{t("download.guardNote")}</DialogDescription>
        </DialogHeader>
        <div className="rounded-md border p-3 text-sm space-y-1">
          <div>{t("download.guardIf")}</div>
          <ul className="text-muted-foreground list-disc pl-5">
            <li>{t("download.guardRest", { site: siteName(shown.site.name), minutes: shown.minutes })}</li>
            {shown.others.map((site) => <li key={site.name}>{otherLine(site)}</li>)}
            <li>{t("download.guardQueue", { done: shown.done, left: shown.left })}</li>
          </ul>
        </div>
        {/* Wait comes first in the DOM so Radix focuses the safe choice; flex-row-reverse keeps it on the right. */}
        <DialogFooter className="sm:flex-row-reverse sm:justify-start">
          <Button disabled={busy} onClick={() => void answer(() => guardWait(shown.site.name))}>{t("download.guardWait", { minutes: shown.minutes })}</Button>
          <Button variant="outline" disabled={busy} onClick={() => void answer(() => guardContinue(shown.site.name))}>{t("download.guardContinue")}</Button>
        </DialogFooter>
      </DialogContent>}
    </Dialog>);
}
