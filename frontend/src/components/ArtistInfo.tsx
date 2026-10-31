import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { BadgeCheck, ExternalLink, XCircle } from "lucide-react";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { DownloadAll, type DownloadCardProps } from "./DownloadButton";
import { ServiceBadge } from "./ServiceBadge";
import type { Service, TrackMetadata } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { useSources } from "@/hooks/useSources";
import { useI18n } from "@/lib/i18n";
import type { AlbumRef, ArtistRef } from "./ArtistLinks";
interface ArtistInfoProps extends DownloadCardProps {
    service: Service;
    artistInfo: {
        name: string;
        images: string;
        header?: string;
        gallery?: string[];
        followers: number;
        biography?: string;
        verified?: boolean;
        listeners?: number;
        rank?: number;
        external_urls: string;
    };
    albumList: Array<{
        id: string;
        name: string;
        images: string;
        release_date: string;
        album_type: string;
        external_urls: string;
        total_tracks?: number;
    }>;
    trackList: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    currentPage: number;
    itemsPerPage: number;
    onSearchChange: (value: string) => void;
    onSortChange: (value: string) => void;
    onAlbumClick: (album: AlbumRef) => void;
    onArtistClick: (artist: ArtistRef) => void;
    onPageChange: (page: number) => void;
    onTrackClick?: (track: TrackMetadata) => void;
    onBack?: () => void;
    activeTab: string;
    onTabChange: (tab: string) => void;
}
export function ArtistInfo({ service, artistInfo, albumList, trackList, searchQuery, sortBy, currentPage, itemsPerPage, onSearchChange, onSortChange, onAlbumClick, onArtistClick, onPageChange, onTrackClick, onBack, activeTab, onTabChange, canDownload, downloadItems, onDownloadAll, downloadProgress, onDownloadTrack, onRetryDownload, onStopDownload, }: ArtistInfoProps) {
    const { t } = useI18n();
    const { label } = useSources();
    const hasGallery = artistInfo.gallery && artistInfo.gallery.length > 0;
    return (<div className="space-y-6">
      <Card className="overflow-hidden p-0 relative">
        {artistInfo.header ? (<>
            <div className="relative w-full min-h-64 bg-cover bg-center">
              <div className="absolute inset-0 bg-cover bg-center" style={{ backgroundImage: `url(${artistInfo.header})` }}/>
              <div className="absolute inset-0 bg-gradient-to-t from-black via-black/50 to-transparent"/>
              {onBack && (<div className="absolute top-4 right-4 z-10">
                  <Button variant="ghost" size="icon" onClick={onBack} aria-label={t("common.close")} className="text-white hover:bg-white/20 hover:text-white">
                      <XCircle className="h-5 w-5"/>
                  </Button>
              </div>)}
              <div className="relative px-6 pt-6 pb-20">
                <div className="flex gap-6 items-start">
                  {artistInfo.images && (<div className="relative group">
                      <img src={artistInfo.images} alt={artistInfo.name} className="w-48 h-48 rounded-full shadow-lg object-cover border-4 border-white"/>
                        </div>)}
                  <div className="flex-1 space-y-2">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-medium text-white/80">{t("type.artist")}</p>
                      <ServiceBadge service={service}/>
                    </div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-4xl font-bold text-white">{artistInfo.name}</h2>
                      {artistInfo.verified && (<BadgeCheck className="h-6 w-6 text-white fill-blue-400 shrink-0"/>)}
                    </div>
                    {artistInfo.biography && (<p className="text-sm text-white/90 line-clamp-4">{artistInfo.biography}</p>)}
                    <div className="flex items-center gap-2 text-sm flex-wrap text-white/90">
                      {artistInfo.rank && (<>
                          <span>{t("artist.rank", { rank: artistInfo.rank })}</span>
                          <span>•</span>
                        </>)}
                      {artistInfo.followers > 0 && (<span>{t("count.followers", { count: artistInfo.followers })}</span>)}
                      {!!artistInfo.listeners && (<>
                          {artistInfo.followers > 0 && (<span>•</span>)}
                          <span>{t("count.listeners", { count: artistInfo.listeners })}</span>
                        </>)}
                    </div>
                    <div className="flex items-center gap-2 text-sm flex-wrap text-white/90">
                      <span>{t("count.albums", { count: albumList.length })}</span>
                      <span>•</span>
                      <span>{t("count.tracks", { count: trackList.length })}</span>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                    <DownloadAll label="download.all" canDownload={canDownload} onDownload={onDownloadAll} progress={downloadProgress} onStop={onStopDownload} size="sm"/>
                    <Button onClick={() => openExternal(artistInfo.external_urls)} size="sm" variant="outline" className="bg-white/10 hover:bg-white/20 text-white border-white/20">
                      <ExternalLink className="h-4 w-4"/>
                      {t("service.open", { service: label(service) })}
                    </Button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </>) : (<CardContent className="px-6 py-6">
            {onBack && (<div className="absolute top-4 right-4 z-10">
                <Button variant="ghost" size="icon" onClick={onBack} aria-label={t("common.close")}>
                    <XCircle className="h-5 w-5"/>
                </Button>
            </div>)}
            <div className="flex gap-6 items-start">
              {artistInfo.images && (<div className="relative group">
                  <img src={artistInfo.images} alt={artistInfo.name} className="w-48 h-48 rounded-full shadow-lg object-cover border-4 border-white"/>
                </div>)}
            <div className="flex-1 space-y-2">
              <div className="flex items-center gap-2">
                <p className="text-sm font-medium">{t("type.artist")}</p>
                <ServiceBadge service={service}/>
              </div>
                <div className="flex items-center gap-2">
              <h2 className="text-4xl font-bold">{artistInfo.name}</h2>
                  {artistInfo.verified && (<BadgeCheck className="h-6 w-6 text-white fill-blue-500 shrink-0"/>)}
                </div>
                {artistInfo.biography && (<p className="text-sm text-muted-foreground line-clamp-4">{artistInfo.biography}</p>)}
              <div className="flex items-center gap-2 text-sm flex-wrap">
                {artistInfo.rank && (<>
                    <span>{t("artist.rank", { rank: artistInfo.rank })}</span>
                    <span>•</span>
                  </>)}
                {artistInfo.followers > 0 && (<span>{t("count.followers", { count: artistInfo.followers })}</span>)}
                {!!artistInfo.listeners && (<>
                    {artistInfo.followers > 0 && (<span>•</span>)}
                    <span>{t("count.listeners", { count: artistInfo.listeners })}</span>
                  </>)}
              </div>
              <div className="flex items-center gap-2 text-sm flex-wrap">
                <span>{t("count.albums", { count: albumList.length })}</span>
                <span>•</span>
                <span>{t("count.tracks", { count: trackList.length })}</span>
              </div>
              <div className="flex flex-wrap items-center gap-2">
              <DownloadAll label="download.all" canDownload={canDownload} onDownload={onDownloadAll} progress={downloadProgress} onStop={onStopDownload} size="sm"/>
              <Button onClick={() => openExternal(artistInfo.external_urls)} size="sm" variant="outline">
                      <ExternalLink className="h-4 w-4"/>
                      {t("service.open", { service: label(service) })}
                    </Button>
              </div>
            </div>
          </div>
        </CardContent>)}
      </Card>

      <div className="border-b">
        <div className="flex gap-6">
            <button onClick={() => onTabChange("albums")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "albums" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                {t("artist.albums")}
            </button>
            <button onClick={() => onTabChange("tracks")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "tracks" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                {t("artist.allTracks")}
            </button>
            {hasGallery && (<button onClick={() => onTabChange("gallery")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "gallery" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                {t("artist.gallery")}
            </button>)}
        </div>
      </div>

      {activeTab === "gallery" && hasGallery && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">{t("artist.galleryCount", { count: artistInfo.gallery!.length })}</h3>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
            {artistInfo.gallery!.map((imageUrl, index) => (<div key={index} className="relative aspect-square rounded-md overflow-hidden shadow-md">
                <img src={imageUrl} alt={t("artist.galleryAlt", { name: artistInfo.name, n: index + 1 })} className="w-full h-full object-cover"/>
              </div>))}
          </div>
        </div>)}

      {activeTab === "albums" && albumList.length > 0 && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">{t("artist.discography")}</h3>
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
            {albumList.map((album) => (<div key={album.id} className="group cursor-pointer relative" onClick={() => onAlbumClick({
                    id: album.id,
                    name: album.name,
                    external_urls: album.external_urls,
                })}>
                <div className="relative mb-2">
                  {album.images && (<img src={album.images} alt={album.name} className="w-full aspect-square object-cover rounded-md shadow-md transition-shadow group-hover:shadow-xl"/>)}
                  <div className="absolute bottom-2 right-2">
                    <span className="text-[10px] uppercase font-bold px-1.5 py-0.5 rounded bg-black/60 text-white backdrop-blur-[2px]">
                        {album.album_type}
                    </span>
                  </div>
                </div>
                <h4 className="font-semibold truncate text-sm">{album.name}</h4>
                <div className="flex items-center gap-2 text-xs text-muted-foreground mt-1">
                    <span>{album.release_date?.split("-")[0]}</span>
                    {!!album.total_tracks && (<>
                            <span>•</span>
                            <span>{t("count.tracks", { count: album.total_tracks })}</span>
                        </>)}
                </div>
              </div>))}
          </div>
        </div>)}

      {activeTab === "tracks" && trackList.length > 0 && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">{t("artist.allTracks")}</h3>
          <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} hasPlays={trackList.some((track) => track.plays)} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
          <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} currentPage={currentPage} itemsPerPage={itemsPerPage} onPageChange={onPageChange} onAlbumClick={onAlbumClick} onArtistClick={onArtistClick} onTrackClick={onTrackClick} canDownload={canDownload} downloadItems={downloadItems} onDownloadTrack={onDownloadTrack} onRetryDownload={onRetryDownload}/>
        </div>)}
    </div>);
}
