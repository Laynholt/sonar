import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { useDownloads } from "@/hooks/useDownloads";
import { siteName } from "@/lib/download-format";
import { useI18n } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetSourceStatus, RunPluginAction } from "../../wailsjs/go/main/App";
import type { source } from "../../wailsjs/go/models";

/** One line per download site; compact packs them side by side. A site that needs an action offers it unless withAction is false. */
export function SourceStatus({ compact, withAction = true }: { compact?: boolean; withAction?: boolean }) {
    const { t } = useI18n();
    const [statuses, setStatuses] = useState<source.Status[]>([]);
    const [checking, setChecking] = useState("");
    const load = useCallback(async () => {
        try { setStatuses(await GetSourceStatus()); }
        catch (error) { console.error("Failed to load source status:", error); }
    }, []);
    // A failed download can mean a site now needs its check; the key changes on status changes only, not on every poll.
    const { items } = useDownloads();
    const statusKey = items.map((item) => item.status).join();
    useEffect(() => { void load(); }, [load, statusKey]);
    const passCheck = async (site: string, action: string) => {
        setChecking(site);
        try {
            await RunPluginAction(site, action);
            toast.success(t("download.checkPassed"));
        }
        catch (error) { toast.error(String(error)); }
        finally {
            setChecking("");
            await load();
        }
    };
    return (<ul className={compact ? "flex flex-wrap gap-x-4 gap-y-1 text-xs" : "space-y-1 text-xs"}>
      {statuses.map((status) => (<li key={status.name} className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{siteName(status)}</span>
        {status.needs_action
            ? <span className="text-muted-foreground">{t("download.siteNeedsCheck")}</span>
            : status.available
                ? <span className="text-muted-foreground">{t("download.siteAvailable")}</span>
                : <span className="text-muted-foreground" title={status.reason}>{t("download.sitePaused")}</span>}
        {withAction && status.needs_action && status.action && (<Button type="button" variant="outline" size="sm" disabled={!!checking} onClick={() => void passCheck(status.name, status.action)}>
          {checking === status.name ? t("download.checkWaiting") : t("download.passCheck")}
        </Button>)}
        {withAction && checking === status.name && (<p className="basis-full text-muted-foreground">{t("download.checkHint")}</p>)}
      </li>))}
    </ul>);
}
