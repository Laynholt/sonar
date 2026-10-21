import { Button } from "@/components/ui/button";
import { ExternalLink, Play, Pause } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious, } from "@/components/ui/pagination";
import type { TrackMetadata } from "@/types/api";
import { usePreview } from "@/hooks/usePreview";
import { openExternal } from "@/lib/utils";
interface TrackListProps {
    tracks: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    currentPage: number;
    itemsPerPage: number;
    hideAlbumColumn?: boolean;
    onPageChange: (page: number) => void;
    onAlbumClick?: (album: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onArtistClick?: (artist: {
        id: string;
        name: string;
        external_urls: string;
    }) => void;
    onTrackClick?: (track: TrackMetadata) => void;
}
export function TrackList({ tracks, searchQuery, sortBy, currentPage, itemsPerPage, hideAlbumColumn = false, onPageChange, onAlbumClick, onArtistClick, onTrackClick, }: TrackListProps) {
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    let filteredTracks = tracks.filter((track) => {
        if (!searchQuery)
            return true;
        const query = searchQuery.toLowerCase();
        return (track.name.toLowerCase().includes(query) ||
            track.artists.toLowerCase().includes(query) ||
            track.album_name.toLowerCase().includes(query));
    });
    if (sortBy === "title-asc") {
        filteredTracks = [...filteredTracks].sort((a, b) => a.name.localeCompare(b.name));
    }
    else if (sortBy === "title-desc") {
        filteredTracks = [...filteredTracks].sort((a, b) => b.name.localeCompare(a.name));
    }
    else if (sortBy === "artist-asc") {
        filteredTracks = [...filteredTracks].sort((a, b) => a.artists.localeCompare(b.artists));
    }
    else if (sortBy === "artist-desc") {
        filteredTracks = [...filteredTracks].sort((a, b) => b.artists.localeCompare(a.artists));
    }
    else if (sortBy === "duration-asc") {
        filteredTracks = [...filteredTracks].sort((a, b) => a.duration_ms - b.duration_ms);
    }
    else if (sortBy === "duration-desc") {
        filteredTracks = [...filteredTracks].sort((a, b) => b.duration_ms - a.duration_ms);
    }
    else if (sortBy === "plays-asc") {
        filteredTracks = [...filteredTracks].sort((a, b) => {
            const aPlays = a.plays ? parseInt(a.plays, 10) : 0;
            const bPlays = b.plays ? parseInt(b.plays, 10) : 0;
            if (isNaN(aPlays))
                return 1;
            if (isNaN(bPlays))
                return -1;
            return aPlays - bPlays;
        });
    }
    else if (sortBy === "plays-desc") {
        filteredTracks = [...filteredTracks].sort((a, b) => {
            const aPlays = a.plays ? parseInt(a.plays, 10) : 0;
            const bPlays = b.plays ? parseInt(b.plays, 10) : 0;
            if (isNaN(aPlays))
                return 1;
            if (isNaN(bPlays))
                return -1;
            return bPlays - aPlays;
        });
    }
    const totalPages = Math.ceil(filteredTracks.length / itemsPerPage);
    const startIndex = (currentPage - 1) * itemsPerPage;
    const endIndex = startIndex + itemsPerPage;
    const paginatedTracks = filteredTracks.slice(startIndex, endIndex);
    const getPaginationPages = (current: number, total: number): (number | 'ellipsis')[] => {
        if (total <= 10) {
            return Array.from({ length: total }, (_, i) => i + 1);
        }
        const pages: (number | 'ellipsis')[] = [];
        pages.push(1);
        if (current <= 7) {
            for (let i = 2; i <= 10; i++) {
                pages.push(i);
            }
            pages.push('ellipsis');
            pages.push(total);
        }
        else if (current >= total - 7) {
            pages.push('ellipsis');
            for (let i = total - 9; i <= total; i++) {
                pages.push(i);
            }
        }
        else {
            pages.push('ellipsis');
            pages.push(current - 1);
            pages.push(current);
            pages.push(current + 1);
            pages.push('ellipsis');
            pages.push(total);
        }
        return pages;
    };
    const formatDuration = (ms: number) => {
        const minutes = Math.floor(ms / 60000);
        const seconds = Math.floor((ms % 60000) / 1000);
        return `${minutes}:${seconds.toString().padStart(2, "0")}`;
    };
    const formatPlays = (plays: string | undefined) => {
        if (!plays)
            return "";
        const num = parseInt(plays, 10);
        if (isNaN(num))
            return plays;
        return num.toLocaleString();
    };
    return (<div className="space-y-4">
    <div className="rounded-md border">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] table-fixed">
          <thead>
            <tr className="border-b bg-muted/50">
              <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground w-12">
                #
              </th>
              <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground">
                Title
              </th>
              {!hideAlbumColumn && (<th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden md:table-cell w-1/4">
                Album
              </th>)}
              <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden lg:table-cell w-24">
                Duration
              </th>
              <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden xl:table-cell w-32">
                Plays
              </th>
              <th className="h-12 px-4 text-center align-middle font-medium text-muted-foreground w-32">
                Actions
              </th>
            </tr>
          </thead>
          <tbody>
            {paginatedTracks.map((track, index) => {
            return (<tr key={index} className="border-b transition-colors hover:bg-muted/50">
              <td className="p-4 align-middle text-sm text-muted-foreground">
                <div className="flex flex-col items-center gap-0.5">
                  <span>{startIndex + index + 1}</span>
                  {track.status && (track.status === "UP" || track.status === "DOWN" || track.status === "NEW") && (<span className={`text-xs ${track.status === "UP"
                    ? "text-green-500"
                    : track.status === "DOWN"
                        ? "text-red-500"
                        : track.status === "NEW"
                            ? "text-blue-500"
                            : ""}`}>
                    {track.status === "NEW" ? "●" : track.status === "UP" ? "▲" : "▼"}
                  </span>)}
                </div>
              </td>
              <td className="p-4 align-middle">
                <div className="flex items-center gap-3">
                  {track.images && (<img src={track.images} alt={track.name} className="w-10 h-10 shrink-0 rounded object-cover"/>)}
                  <div className="flex min-w-0 flex-col [overflow-wrap:anywhere]">
                    <div className="flex items-center gap-2">
                      {onTrackClick ? (<span className="font-medium cursor-pointer hover:underline" onClick={() => onTrackClick(track)}>
                        {track.name}
                      </span>) : (<span className="font-medium">{track.name}</span>)}
                      {track.is_explicit && (<span className="inline-flex items-center justify-center bg-red-600 text-white text-[10px] h-4 w-4 rounded shrink-0" title="Explicit">E</span>)}
                    </div>
                    <span className="text-sm text-muted-foreground">
                      {track.artists_data && track.artists_data.length > 0 ? ((() => {
                const artistNames = track.artists.split(", ").map(name => name.trim());
                return artistNames.map((name, i) => {
                    const artistData = track.artists_data![i];
                    const hasArtistData = artistData && artistData.id && artistData.external_urls;
                    return (<span key={artistData?.id || i}>
                            {onArtistClick && hasArtistData ? (<span className="cursor-pointer hover:underline" onClick={() => onArtistClick({
                                id: artistData.id,
                                name: name,
                                external_urls: artistData.external_urls,
                            })}>
                              {name}
                            </span>) : (name)}
                            {i < artistNames.length - 1 && ", "}
                          </span>);
                });
            })()) : onArtistClick && track.artist_id && track.artist_url ? (<span className="cursor-pointer hover:underline" onClick={() => onArtistClick({
                    id: track.artist_id!,
                    name: track.artists,
                    external_urls: track.artist_url!,
                })}>
                        {track.artists}
                      </span>) : (track.artists)}
                    </span>
                  </div>
                </div>
              </td>
              {!hideAlbumColumn && (<td className="p-4 align-middle text-sm text-muted-foreground hidden md:table-cell [overflow-wrap:anywhere]">
                {onAlbumClick && track.album_id && track.album_url ? (<span className="cursor-pointer hover:underline" onClick={() => onAlbumClick({
                        id: track.album_id!,
                        name: track.album_name,
                        external_urls: track.album_url!,
                    })}>
                  {track.album_name}
                </span>) : (track.album_name)}
              </td>)}
              <td className="p-4 align-middle text-sm text-muted-foreground hidden lg:table-cell">
                {formatDuration(track.duration_ms)}
              </td>
              <td className="p-4 align-middle text-sm text-muted-foreground hidden xl:table-cell">
                {track.plays ? formatPlays(track.plays) : ""}
              </td>
              <td className="p-4 align-middle text-center">
                <div className="flex items-center justify-center gap-1">
                  {track.external_urls && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button onClick={() => openExternal(track.external_urls)} size="icon" variant="outline" aria-label="Open in Spotify">
                        <ExternalLink className="h-4 w-4"/>
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>Open in Spotify</p>
                    </TooltipContent>
                  </Tooltip>)}
                  {track.id && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button onClick={() => playPreview(track.id!, track.name)} size="icon" variant="outline" disabled={loadingPreview === track.id}>
                        {loadingPreview === track.id ? (<Spinner />) : playingTrack === track.id ? (<Pause className="h-4 w-4"/>) : (<Play className="h-4 w-4"/>)}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{playingTrack === track.id ? "Stop Preview" : "Play Preview"}</p>
                    </TooltipContent>
                  </Tooltip>)}
                </div>
              </td>
            </tr>);
        })}
          </tbody>
        </table>
      </div>
    </div>

    {totalPages > 1 && (<Pagination>
      <PaginationContent>
        <PaginationItem>
          <PaginationPrevious href="#" onClick={(e) => {
                e.preventDefault();
                if (currentPage > 1)
                    onPageChange(currentPage - 1);
            }} className={currentPage === 1 ? "pointer-events-none opacity-50" : "cursor-pointer"}/>
        </PaginationItem>

        {getPaginationPages(currentPage, totalPages).map((page, index) => (page === 'ellipsis' ? (<PaginationItem key={`ellipsis-${index}`}>
              <PaginationEllipsis />
            </PaginationItem>) : (<PaginationItem key={page}>
              <PaginationLink href="#" onClick={(e) => {
                    e.preventDefault();
                    onPageChange(page);
                }} isActive={currentPage === page} className="cursor-pointer">
                {page}
              </PaginationLink>
            </PaginationItem>)))}

        <PaginationItem>
          <PaginationNext href="#" onClick={(e) => {
                e.preventDefault();
                if (currentPage < totalPages)
                    onPageChange(currentPage + 1);
            }} className={currentPage === totalPages
                ? "pointer-events-none opacity-50"
                : "cursor-pointer"}/>
        </PaginationItem>
      </PaginationContent>
    </Pagination>)}
  </div>);
}
