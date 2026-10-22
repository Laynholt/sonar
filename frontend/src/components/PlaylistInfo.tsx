import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ExternalLink, XCircle } from "lucide-react";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { ServiceBadge, serviceLabels } from "./ServiceBadge";
import type { Service, TrackMetadata } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { useI18n } from "@/lib/i18n";
import type { AlbumRef, ArtistRef } from "./ArtistLinks";
import { DownloadAll, type DownloadCardProps } from "./DownloadButton";
interface PlaylistInfoProps extends DownloadCardProps {
    service: Service;
    sourceUrl: string;
    playlistInfo: {
        owner: {
            name: string;
            display_name: string;
            images: string;
        };
        tracks: {
            total: number;
        };
        followers: {
            total: number;
        };
        cover?: string;
        description?: string;
    };
    trackList: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    currentPage: number;
    itemsPerPage: number;
    onSearchChange: (value: string) => void;
    onSortChange: (value: string) => void;
    onPageChange: (page: number) => void;
    onAlbumClick: (album: AlbumRef) => void;
    onArtistClick: (artist: ArtistRef) => void;
    onTrackClick: (track: TrackMetadata) => void;
    onBack?: () => void;
}
function decodeHtmlEntities(value?: string): string {
    if (!value) {
        return "";
    }

    if (typeof document === "undefined") {
        return value
            .replace(/&#x2F;/gi, "/")
            .replace(/&lt;/gi, "<")
            .replace(/&gt;/gi, ">")
            .replace(/&amp;/gi, "&")
            .replace(/&quot;/gi, "\"")
            .replace(/&#39;/g, "'");
    }

    const textarea = document.createElement("textarea");
    textarea.innerHTML = value;
    return textarea.value;
}
export function PlaylistInfo({ service, sourceUrl, playlistInfo, trackList, searchQuery, sortBy, currentPage, itemsPerPage, onSearchChange, onSortChange, onPageChange, onAlbumClick, onArtistClick, onTrackClick, onBack, canDownload, downloadItems, onDownloadAll, downloadProgress, onDownloadTrack, onRetryDownload, onStopDownload, }: PlaylistInfoProps) {
    const { t } = useI18n();
    const decodedDescription = decodeHtmlEntities(playlistInfo.description);
    return (<div className="space-y-6">
      <Card className="relative">
      {onBack && (<div className="absolute top-4 right-4 z-10">
          <Button variant="ghost" size="icon" onClick={onBack} aria-label={t("common.close")}>
              <XCircle className="h-5 w-5"/>
          </Button>
      </div>)}
        <CardContent className="px-6">
          <div className="flex gap-6 items-start">
            {playlistInfo.cover && (<img src={playlistInfo.cover} alt={playlistInfo.owner.name} className="w-48 h-48 rounded-md shadow-lg object-cover"/>)}
            <div className="flex-1 space-y-4">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <p className="text-sm font-medium">{t("type.playlist")}</p>
                  <ServiceBadge service={service}/>
                </div>
                <h2 className="text-4xl font-bold">{playlistInfo.owner.name}</h2>
                {decodedDescription && (<p className="text-sm text-muted-foreground">{decodedDescription}</p>)}
                <div className="flex items-center gap-2 text-sm">
                  <div className="flex items-center gap-2">
                    {playlistInfo.owner.images && (<img src={playlistInfo.owner.images} alt={playlistInfo.owner.display_name} className="w-5 h-5 rounded-full object-cover"/>)}
                    <span className="font-medium">{playlistInfo.owner.display_name}</span>
                  </div>
                  <span>•</span>
                  <span>
                    {t("count.tracks", { count: playlistInfo.tracks.total })}
                  </span>
                  {playlistInfo.followers.total > 0 && (<>
                      <span>•</span>
                      <span>{t("count.followers", { count: playlistInfo.followers.total })}</span>
                    </>)}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <DownloadAll label="download.all" canDownload={canDownload} onDownload={onDownloadAll} progress={downloadProgress} onStop={onStopDownload}/>
                <Button onClick={() => openExternal(sourceUrl)} variant="outline">
                  <ExternalLink className="h-4 w-4"/>
                  {t("service.open", { service: serviceLabels[service] })}
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
      <div className="space-y-4">
        <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} hasPlays={trackList.some((track) => track.plays)} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
        <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} currentPage={currentPage} itemsPerPage={itemsPerPage} onPageChange={onPageChange} onAlbumClick={onAlbumClick} onArtistClick={onArtistClick} onTrackClick={onTrackClick} canDownload={canDownload} downloadItems={downloadItems} onDownloadTrack={onDownloadTrack} onRetryDownload={onRetryDownload}/>
      </div>
    </div>);
}
