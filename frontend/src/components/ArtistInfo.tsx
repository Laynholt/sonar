import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { BadgeCheck, ExternalLink, XCircle } from "lucide-react";
import { SearchAndSort } from "./SearchAndSort";
import { TrackList } from "./TrackList";
import { ServiceBadge, serviceLabels } from "./ServiceBadge";
import type { Service, TrackMetadata } from "@/types/api";
import { openExternal } from "@/lib/utils";
import { useState } from "react";
import type { AlbumRef, ArtistRef } from "./ArtistLinks";
interface ArtistInfoProps {
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
}
export function ArtistInfo({ service, artistInfo, albumList, trackList, searchQuery, sortBy, currentPage, itemsPerPage, onSearchChange, onSortChange, onAlbumClick, onArtistClick, onPageChange, onTrackClick, onBack, }: ArtistInfoProps) {
    const [activeTab, setActiveTab] = useState<"albums" | "tracks" | "gallery">("albums");
    const hasGallery = artistInfo.gallery && artistInfo.gallery.length > 0;
    return (<div className="space-y-6">
      <Card className="overflow-hidden p-0 relative">
        {artistInfo.header ? (<>
            <div className="relative w-full min-h-64 bg-cover bg-center">
              <div className="absolute inset-0 bg-cover bg-center" style={{ backgroundImage: `url(${artistInfo.header})` }}/>
              <div className="absolute inset-0 bg-gradient-to-t from-black via-black/50 to-transparent"/>
              {onBack && (<div className="absolute top-4 right-4 z-10">
                  <Button variant="ghost" size="icon" onClick={onBack} className="text-white hover:bg-white/20 hover:text-white">
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
                      <p className="text-sm font-medium text-white/80">Artist</p>
                      <ServiceBadge service={service}/>
                    </div>
                    <div className="flex items-center gap-2">
                      <h2 className="text-4xl font-bold text-white">{artistInfo.name}</h2>
                      {artistInfo.verified && (<BadgeCheck className="h-6 w-6 text-white fill-blue-400 shrink-0"/>)}
                    </div>
                    {artistInfo.biography && (<p className="text-sm text-white/90 line-clamp-4">{artistInfo.biography}</p>)}
                    <div className="flex items-center gap-2 text-sm flex-wrap text-white/90">
                      {artistInfo.rank && (<>
                          <span>#{artistInfo.rank} rank</span>
                          <span>•</span>
                        </>)}
                      <span>{artistInfo.followers.toLocaleString()} {artistInfo.followers === 1 ? "follower" : "followers"}</span>
                      {artistInfo.listeners && (<>
                          <span>•</span>
                          <span>{artistInfo.listeners.toLocaleString()} {artistInfo.listeners === 1 ? "listener" : "listeners"}</span>
                        </>)}
                    </div>
                    <div className="flex items-center gap-2 text-sm flex-wrap text-white/90">
                      <span>{albumList.length} {albumList.length === 1 ? "album" : "albums"}</span>
                      <span>•</span>
                      <span>{trackList.length} {trackList.length === 1 ? "track" : "tracks"}</span>
                    </div>
                    <Button onClick={() => openExternal(artistInfo.external_urls)} size="sm" variant="outline" className="bg-white/10 hover:bg-white/20 text-white border-white/20">
                      <ExternalLink className="h-4 w-4"/>
                      Open in {serviceLabels[service]}
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          </>) : (<CardContent className="px-6 py-6">
            {onBack && (<div className="absolute top-4 right-4 z-10">
                <Button variant="ghost" size="icon" onClick={onBack}>
                    <XCircle className="h-5 w-5"/>
                </Button>
            </div>)}
            <div className="flex gap-6 items-start">
              {artistInfo.images && (<div className="relative group">
                  <img src={artistInfo.images} alt={artistInfo.name} className="w-48 h-48 rounded-full shadow-lg object-cover border-4 border-white"/>
                </div>)}
            <div className="flex-1 space-y-2">
              <div className="flex items-center gap-2">
                <p className="text-sm font-medium">Artist</p>
                <ServiceBadge service={service}/>
              </div>
                <div className="flex items-center gap-2">
              <h2 className="text-4xl font-bold">{artistInfo.name}</h2>
                  {artistInfo.verified && (<BadgeCheck className="h-6 w-6 text-white fill-blue-500 shrink-0"/>)}
                </div>
                {artistInfo.biography && (<p className="text-sm text-muted-foreground line-clamp-4">{artistInfo.biography}</p>)}
              <div className="flex items-center gap-2 text-sm flex-wrap">
                {artistInfo.rank && (<>
                    <span>#{artistInfo.rank} rank</span>
                    <span>•</span>
                  </>)}
                <span>{artistInfo.followers.toLocaleString()} {artistInfo.followers === 1 ? "follower" : "followers"}</span>
                {artistInfo.listeners && (<>
                    <span>•</span>
                    <span>{artistInfo.listeners.toLocaleString()} {artistInfo.listeners === 1 ? "listener" : "listeners"}</span>
                  </>)}
              </div>
              <div className="flex items-center gap-2 text-sm flex-wrap">
                <span>{albumList.length} {albumList.length === 1 ? "album" : "albums"}</span>
                <span>•</span>
                <span>{trackList.length} {trackList.length === 1 ? "track" : "tracks"}</span>
              </div>
              <Button onClick={() => openExternal(artistInfo.external_urls)} size="sm" variant="outline">
                      <ExternalLink className="h-4 w-4"/>
                      Open in {serviceLabels[service]}
                    </Button>
            </div>
          </div>
        </CardContent>)}
      </Card>

      <div className="border-b">
        <div className="flex gap-6">
            <button onClick={() => setActiveTab("albums")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "albums" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                Albums
            </button>
            <button onClick={() => setActiveTab("tracks")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "tracks" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                All Tracks
            </button>
            {hasGallery && (<button onClick={() => setActiveTab("gallery")} className={`pb-3 text-sm font-medium transition-colors border-b-2 -mb-px hover:text-foreground ${activeTab === "gallery" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}>
                Gallery
            </button>)}
        </div>
      </div>

      {activeTab === "gallery" && hasGallery && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">Gallery ({artistInfo.gallery!.length})</h3>
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4">
            {artistInfo.gallery!.map((imageUrl, index) => (<div key={index} className="relative aspect-square rounded-md overflow-hidden shadow-md">
                <img src={imageUrl} alt={`${artistInfo.name} gallery ${index + 1}`} className="w-full h-full object-cover"/>
              </div>))}
          </div>
        </div>)}

      {activeTab === "albums" && albumList.length > 0 && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">Discography</h3>
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
                    {album.total_tracks && (<>
                            <span>•</span>
                            <span>{album.total_tracks} {album.total_tracks === 1 ? "track" : "tracks"}</span>
                        </>)}
                </div>
              </div>))}
          </div>
        </div>)}

      {activeTab === "tracks" && trackList.length > 0 && (<div className="space-y-4">
          <h3 className="text-2xl font-bold">All Tracks</h3>
          <SearchAndSort searchQuery={searchQuery} sortBy={sortBy} onSearchChange={onSearchChange} onSortChange={onSortChange}/>
          <TrackList tracks={trackList} searchQuery={searchQuery} sortBy={sortBy} currentPage={currentPage} itemsPerPage={itemsPerPage} onPageChange={onPageChange} onAlbumClick={onAlbumClick} onArtistClick={onArtistClick} onTrackClick={onTrackClick}/>
        </div>)}
    </div>);
}
