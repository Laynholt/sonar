import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ExternalLink, XCircle, Play, Pause } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { TrackMetadata } from "@/types/api";
import { usePreview } from "@/hooks/usePreview";
import { openExternal } from "@/lib/utils";
import { ServiceBadge, serviceLabels } from "./ServiceBadge";
interface TrackInfoProps {
    track: TrackMetadata & {
        album_name: string;
        release_date: string;
    };
    onBack?: () => void;
}
export function TrackInfo({ track, onBack, }: TrackInfoProps) {
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    const formatDuration = (ms: number) => {
        const minutes = Math.floor(ms / 60000);
        const seconds = Math.floor((ms % 60000) / 1000);
        return `${minutes}:${seconds.toString().padStart(2, "0")}`;
    };
    const formatPlays = (plays: string) => {
        const num = parseInt(plays, 10);
        if (isNaN(num))
            return plays;
        return num.toLocaleString();
    };
    return (<Card className="relative">
    {onBack && (<div className="absolute top-4 right-4 z-10">
        <Button variant="ghost" size="icon" onClick={onBack} aria-label="Back">
            <XCircle className="h-5 w-5"/>
        </Button>
    </div>)}
    <CardContent className="px-6">
      <div className="flex gap-6 items-start">
        {track.images && (<div className="shrink-0">
          <div className="relative w-48 h-48 rounded-md shadow-lg overflow-hidden">
            <img src={track.images} alt={track.name} className="w-full h-full object-cover"/>
            <div className="absolute bottom-1 right-1 bg-black/80 text-white px-1.5 py-0.5 text-xs font-medium rounded">
              {formatDuration(track.duration_ms)}
            </div>
          </div>
        </div>)}
        <div className="flex-1 space-y-4 min-w-0">
          <div className="space-y-1">
            <ServiceBadge service={track.service}/>
            <div className="flex items-center gap-3">
              <h1 className="text-3xl font-bold wrap-break-word">{track.name}</h1>
              {track.is_explicit && (<span className="inline-flex items-center justify-center bg-red-600 text-white text-[10px] h-4 w-4 rounded shrink-0" title="Explicit">E</span>)}
            </div>
            <p className="text-lg text-muted-foreground">{track.artists}</p>
          </div>
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div className="space-y-1">
              <div>
                <p className="text-xs text-muted-foreground">Album</p>
                <p className="font-medium truncate">{track.album_name}</p>
              </div>
              {track.plays && (<div>
                <p className="text-xs text-muted-foreground">Total Plays</p>
                <p className="font-medium">{formatPlays(track.plays)}</p>
              </div>)}
            </div>
            <div className="space-y-1">
              <div>
                <p className="text-xs text-muted-foreground">Release Date</p>
                <p className="font-medium">{track.release_date}</p>
              </div>
              {track.copyright && (<div>
                <p className="text-xs text-muted-foreground">Copyright</p>
                <p className="font-medium truncate" title={track.copyright}>
                  {track.copyright}
                </p>
              </div>)}
            </div>
          </div>
          <div className="flex gap-2">
            <Button onClick={() => openExternal(track.external_urls)} variant="outline">
              <ExternalLink className="h-4 w-4"/>
              Open in {serviceLabels[track.service]}
            </Button>
            {track.service === "spotify" && track.id && (<Tooltip>
              <TooltipTrigger asChild>
                <Button onClick={() => playPreview(track.id!, track.name)} variant="outline" size="icon" aria-label={playingTrack === track.id ? "Stop preview" : "Play preview"} disabled={loadingPreview === track.id}>
                  {loadingPreview === track.id ? (<Spinner />) : playingTrack === track.id ? (<Pause className="h-4 w-4"/>) : (<Play className="h-4 w-4"/>)}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{playingTrack === track.id ? "Stop Preview" : "Play Preview"}</p>
              </TooltipContent>
            </Tooltip>)}
          </div>
        </div>
      </div>
    </CardContent>
  </Card>);
}
