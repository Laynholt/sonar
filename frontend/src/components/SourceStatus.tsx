import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetSourceStatus, PassDoubleDoubleCheck } from "../../wailsjs/go/main/App";
import type { source } from "../../wailsjs/go/models";

/** One line per download site; compact packs them side by side. */
export function SourceStatus({ compact }: { compact?: boolean }) {
    const { t } = useI18n();
    const [statuses, setStatuses] = useState<source.Status[]>([]);
    const [checking, setChecking] = useState(false);
    const load = useCallback(async () => {
        try { setStatuses(await GetSourceStatus()); }
        catch (error) { console.error("Failed to load source status:", error); }
    }, []);
    useEffect(() => { void load(); }, [load]);
    const passCheck = async () => {
        setChecking(true);
        try {
            await PassDoubleDoubleCheck();
            toast.success(t("download.checkPassed"));
        }
        catch (error) { toast.error(String(error)); }
        finally {
            setChecking(false);
            await load();
        }
    };
    return (<ul className={compact ? "flex flex-wrap gap-x-4 gap-y-1 text-xs" : "space-y-1 text-xs"}>
      {statuses.map((status) => (<li key={status.name} className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{status.name}</span>
        {status.needs_check
            ? <span className="text-muted-foreground">{t("download.siteNeedsCheck")}</span>
            : status.available
                ? <span className="text-muted-foreground">{t("download.siteAvailable")}</span>
                : <span className="text-muted-foreground" title={status.reason}>{t("download.sitePaused")}</span>}
        {status.name === "doubledouble" && status.needs_check && (<Button type="button" variant="outline" size="sm" disabled={checking} onClick={passCheck}>
          {checking ? t("download.checkWaiting") : t("download.passCheck")}
        </Button>)}
      </li>))}
    </ul>);
}
