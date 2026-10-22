import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { clearQueue, resumeQueue } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import { logger } from "@/lib/logger";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import { GetPendingQueue } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

const SHOWN_NAMES = 3;

/** Offers to continue the queue left from the last run. Asks once, when the app starts. */
export function ResumeDialog() {
    const { t } = useI18n();
    const [pending, setPending] = useState<main.PendingQueue | null>(null);
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        let alive = true;
        GetPendingQueue().then((queue) => { if (alive && queue.tracks > 0) setPending(queue); })
            .catch((err) => logger.error(errorMessage(err, "pending queue check failed")));
        return () => { alive = false; };
    }, []);

    const answer = async (action: () => Promise<void>) => {
        setBusy(true);
        try { await action(); setPending(null); }
        catch (err) { toast.error(errorMessage(err, t("recent.unknownError"))); }
        finally { setBusy(false); }
    };

    const names = pending?.collections ?? [];
    const quoted = names.slice(0, SHOWN_NAMES).map((name) => `«${name}»`).join(", ");
    const more = names.length > SHOWN_NAMES ? ` ${t("download.resumeMore", { n: names.length - SHOWN_NAMES })}` : "";
    const from = names.length ? t("download.resumeFrom", { names: quoted + more }) : "";
    return (<Dialog open={!!pending} onOpenChange={(open) => { if (!open && !busy) setPending(null); }}>
      {pending && <DialogContent showCloseButton={false} className="sm:max-w-[460px]">
        <DialogHeader>
          <DialogTitle>{t("download.resumeTitle")}</DialogTitle>
          <DialogDescription>
            {t("download.resumeText", { count: pending.tracks, from })}
            {pending.review > 0 && ` ${t("download.resumeReview", { count: pending.review })}`}
          </DialogDescription>
        </DialogHeader>
        {/* Continue comes first in the DOM so Radix focuses it, not the destructive clear; flex-row-reverse keeps it on the right. */}
        <DialogFooter className="sm:flex-row-reverse sm:justify-start">
          <Button disabled={busy} onClick={() => void answer(resumeQueue)}>{t("download.resume")}</Button>
          <Button variant="outline" disabled={busy} onClick={() => setPending(null)}>{t("download.resumeLater")}</Button>
          <Button variant="outline" disabled={busy} onClick={() => void answer(clearQueue)}>{t("download.resumeClear")}</Button>
        </DialogFooter>
      </DialogContent>}
    </Dialog>);
}
