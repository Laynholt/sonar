import { useCallback, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { useDownloads } from "@/hooks/useDownloads";
import { siteName } from "@/lib/download-format";
import { useI18n } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetSourceStatus, RunPluginAction } from "../../wailsjs/go/main/App";
import type { source } from "../../wailsjs/go/models";

/** One line per download site; compact packs them side by side. A site that needs an action offers it unless withAction is false; onlyNeeding lists just those sites, in a box, and nothing when there are none. */
export function SourceStatus({ compact, withAction = true, onlyNeeding }: { compact?: boolean; withAction?: boolean; onlyNeeding?: boolean }) {
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
    const passCheck = async (status: source.Status) => {
        const site = status.name;
        setChecking(site);
        try {
            await RunPluginAction(site, status.action);
            toast.success(t("download.checkPassed", { site: siteName(status) }));
        }
        catch (error) { toast.error(String(error)); }
        finally {
            setChecking("");
            await load();
        }
    };
    // The balancer prefixes the reason with the site id, which this list already names.
    const why = (status: source.Status) => (status.reason.startsWith(`${status.name}: `) ? status.reason.slice(status.name.length + 2) : status.reason);
    const shown = onlyNeeding ? statuses.filter((status) => status.needs_action) : statuses;
    if (onlyNeeding && !shown.length) return null;
    const list = (<ul className={compact ? "flex flex-wrap gap-x-4 gap-y-1 text-xs" : "space-y-1 text-xs"}>
      {shown.map((status) => (<li key={status.name} className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{siteName(status)}</span>
        {status.needs_action
            ? <span className="text-muted-foreground">{t("download.siteNeedsCheck")}</span>
            : status.available
                ? <span className="text-muted-foreground">{t("download.siteAvailable")}</span>
                : <span className="text-muted-foreground" title={why(status)}>{t("download.sitePaused")}</span>}
        {withAction && status.needs_action && status.action && (<Button type="button" variant="outline" size="sm" disabled={!!checking} onClick={() => void passCheck(status)}>
          {checking === status.name ? t("download.checkWaiting") : t("download.passCheck")}
        </Button>)}
        {withAction && checking === status.name && status.reason && (<p className="basis-full text-muted-foreground">{why(status)}</p>)}
      </li>))}
    </ul>);
    return onlyNeeding ? <div className="rounded-md border p-4">{list}</div> : list;
}
