import { Music, Pause, Play } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { cn } from "@/lib/utils";
import { usePreview } from "@/hooks/usePreview";
import { useSources } from "@/hooks/useSources";
import { useI18n } from "@/lib/i18n";
interface CoverPreviewProps {
    service: string;
    trackId?: string;
    trackName: string;
    src?: string;
    alt?: string;
    className?: string;
}
export function CoverPreview({ service, trackId, trackName, src, alt, className }: CoverPreviewProps) {
    const { t } = useI18n();
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    const { can } = useSources();
    const loading = !!trackId && loadingPreview === trackId;
    const playing = !!trackId && playingTrack === trackId;
    return (<div className={cn("group relative shrink-0 overflow-hidden rounded bg-muted", className)}>
    {src ? (<img src={src} alt={alt ?? trackName} className="h-full w-full object-cover"/>) : (<div className="flex h-full w-full items-center justify-center text-muted-foreground"><Music className="h-1/2 w-1/2"/></div>)}
    {trackId && can(service, "preview") && (<button type="button" disabled={loading} aria-label={playing ? t("preview.stop") : t("preview.play")} onClick={(e) => {
                e.stopPropagation();
                void playPreview(service, trackId, trackName);
            }} className={cn("absolute inset-0 flex cursor-pointer items-center justify-center bg-black/50 text-white transition-opacity", loading || playing ? "opacity-100" : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100")}>
      {loading ? <Spinner /> : playing ? <Pause className="h-6 w-6"/> : <Play className="h-6 w-6"/>}
    </button>)}
  </div>);
}
