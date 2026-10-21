import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { ExternalLink, XCircle } from "lucide-react";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { ServiceBadge, serviceLabels } from "./ServiceBadge";
import type { Service, TrackMetadata } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { useI18n } from "@/lib/i18n";
import type { ArtistRef } from "./ArtistLinks";
interface AlbumInfoProps {
    service: Service;
    sourceUrl: string;
    albumInfo: {
        name: string;
        artists: string;
        images: string;
        release_date: string;
        total_tracks: number;
        artist_id?: string;
        artist_url?: string;
    };
    trackList: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    currentPage: number;
    itemsPerPage: number;
    onSearchChange: (value: string) => void;
    onSortChange: (value: string) => void;
    onPageChange: (page: number) => void;
    onArtistClick?: (artist: ArtistRef) => void;
    onTrackClick?: (track: TrackMetadata) => void;
    onBack?: () => void;
}
export function AlbumInfo({ service, sourceUrl, albumInfo, trackList, searchQuery, sortBy, currentPage, itemsPerPage, onSearchChange, onSortChange, onPageChange, onArtistClick, onTrackClick, onBack, }: AlbumInfoProps) {
    const { t } = useI18n();
    const resolvedAlbumArtist = (() => {
        if (albumInfo.artist_id && albumInfo.artist_url) {
            return {
                id: albumInfo.artist_id,
                name: albumInfo.artists,
                external_urls: albumInfo.artist_url,
            };
        }

        for (const track of trackList) {
            if (track.artists_data && track.artists_data.length > 0) {
                const first = track.artists_data[0];
                if (first?.id && first?.external_urls) {
                    return {
                        id: first.id,
                        name: albumInfo.artists || track.artists,
                        external_urls: first.external_urls,
                    };
                }
            }
            if (track.artist_id && track.artist_url) {
                return {
                    id: track.artist_id,
                    name: albumInfo.artists || track.artists,
                    external_urls: track.artist_url,
                };
            }
        }

        return null;
    })();

    return (<div className="space-y-6">
      <Card className="relative">
      {onBack && (<div className="absolute top-4 right-4 z-10">
          <Button variant="ghost" size="icon" onClick={onBack}>
              <XCircle className="h-5 w-5"/>
          </Button>
      </div>)}
        <CardContent className="px-6">
          <div className="flex gap-6 items-start">
            {albumInfo.images && (<img src={albumInfo.images} alt={albumInfo.name} className="w-48 h-48 rounded-md shadow-lg object-cover"/>)}
            <div className="flex-1 space-y-4">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <p className="text-sm font-medium">{t("type.album")}</p>
                  <ServiceBadge service={service}/>
                </div>
                <h2 className="text-4xl font-bold">{albumInfo.name}</h2>
                <div className="flex items-center gap-2 text-sm">
                  {onArtistClick && resolvedAlbumArtist ? (<span className="font-medium cursor-pointer hover:underline" onClick={() => onArtistClick({
                id: resolvedAlbumArtist.id,
                name: resolvedAlbumArtist.name,
                external_urls: resolvedAlbumArtist.external_urls,
            })}>
                      {albumInfo.artists}
                    </span>) : (<span className="font-medium">{albumInfo.artists}</span>)}
                  <span>•</span>
                  <span>{albumInfo.release_date}</span>
                  <span>•</span>
                  <span>
                    {t("count.tracks", { count: albumInfo.total_tracks })}
                  </span>
                </div>
              </div>
              <div className="flex gap-2">
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
        <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
        <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} currentPage={currentPage} itemsPerPage={itemsPerPage} hideAlbumColumn={true} onPageChange={onPageChange} onArtistClick={onArtistClick} onTrackClick={onTrackClick}/>
      </div>
    </div>);
}
