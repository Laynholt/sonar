import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useDownloads } from "@/hooks/useDownloads";
import { guardContinue, guardWait } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";

/** Asks what to do at the rate limit. Controlled with no onOpenChange, so Escape and outside clicks do nothing. */
export function GuardDialog() {
    const { t } = useI18n();
    const { guard } = useDownloads();
    const [busy, setBusy] = useState(false);
    const answer = async (action: () => Promise<void>) => {
        setBusy(true);
        try { await action(); }
        catch (err) { toast.error(errorMessage(err, t("recent.unknownError"))); }
        finally { setBusy(false); }
    };
    return (<Dialog open={guard.asking}>
      <DialogContent showCloseButton={false} className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{t("download.guardTitle")}</DialogTitle>
          <DialogDescription>{t("download.guardText", { count: guard.count, site: guard.site })}</DialogDescription>
        </DialogHeader>
        {/* Wait comes first in the DOM so Radix focuses the safe choice; flex-row-reverse keeps it on the right. */}
        <DialogFooter className="sm:flex-row-reverse sm:justify-start">
          <Button disabled={busy} onClick={() => void answer(guardWait)}>{t("download.guardWait")}</Button>
          <Button variant="outline" disabled={busy} onClick={() => void answer(guardContinue)}>{t("download.guardContinue")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>);
}
