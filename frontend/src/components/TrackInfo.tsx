import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ExternalLink, XCircle } from "lucide-react";
import type { TrackMetadata } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { AlbumLink, ArtistLinks, type AlbumRef, type ArtistRef } from "./ArtistLinks";
import { CoverPreview } from "./CoverPreview";
import { ServiceBadge, serviceLabels } from "./ServiceBadge";
interface TrackInfoProps {
    track: TrackMetadata & {
        album_name: string;
        release_date: string;
    };
    onBack?: () => void;
    onArtistClick?: (artist: ArtistRef) => void;
    onAlbumClick?: (album: AlbumRef) => void;
}
export function TrackInfo({ track, onBack, onArtistClick, onAlbumClick, }: TrackInfoProps) {
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
    const details = [
        ["Release Date", track.release_date],
        ["Plays", track.plays && formatPlays(track.plays)],
        ["Duration", formatDuration(track.duration_ms)],
        ["Copyright", track.copyright],
    ].filter(([, value]) => value);
    return (<Card className="relative">
    {onBack && (<div className="absolute top-4 right-4 z-10">
        <Button variant="ghost" size="icon" onClick={onBack} aria-label="Back">
            <XCircle className="h-5 w-5"/>
        </Button>
    </div>)}
    <CardContent className="px-6">
      <div className="flex flex-col gap-6 md:flex-row">
        <div className="flex flex-1 items-start gap-6 min-w-0">
          <CoverPreview trackId={track.id} trackName={track.name} src={track.images} className="h-48 w-48 rounded-md shadow-lg"/>
          <div className="flex min-h-48 flex-1 flex-col justify-between gap-4 min-w-0">
            <div className="space-y-1">
              <ServiceBadge service={track.service}/>
              <div className="flex items-center gap-3">
                <h1 className="text-3xl font-bold wrap-break-word">{track.name}</h1>
                {track.is_explicit && (<span className="inline-flex items-center justify-center bg-red-600 text-white text-[10px] h-4 w-4 rounded shrink-0" title="Explicit">E</span>)}
              </div>
              <p className="text-lg text-muted-foreground">
                <ArtistLinks track={track} onArtistClick={onArtistClick}/> · <AlbumLink track={track} onAlbumClick={onAlbumClick}/>
              </p>
            </div>
            <div>
              <Button onClick={() => openExternal(track.external_urls)} variant="outline">
                <ExternalLink className="h-4 w-4"/>
                Open in {serviceLabels[track.service]}
              </Button>
            </div>
          </div>
        </div>
        <div className="space-y-3 border-t pt-4 text-sm md:w-56 md:shrink-0 md:border-l md:border-t-0 md:pl-6 md:pt-0 md:pr-8">
          {details.map(([label, value]) => (<div key={label}>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="font-medium wrap-break-word">{value}</p>
          </div>))}
        </div>
      </div>
    </CardContent>
  </Card>);
}
