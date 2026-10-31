import { Button } from "@/components/ui/button";
import { ExternalLink } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious, } from "@/components/ui/pagination";
import type { TrackMetadata } from "@/types/api";
import { AlbumLink, ArtistLinks, type AlbumRef, type ArtistRef } from "./ArtistLinks";
import { CoverPreview } from "./CoverPreview";
import { openExternal } from "@/lib/utils";
import { useSources } from "@/hooks/useSources";
import { useI18n } from "@/lib/i18n";
import { DownloadButton, type DownloadRowProps } from "./DownloadButton";
interface TrackListProps extends DownloadRowProps {
    tracks: TrackMetadata[];
    searchQuery: string;
    sortBy: string;
    currentPage: number;
    itemsPerPage: number;
    hideAlbumColumn?: boolean;
    onPageChange: (page: number) => void;
    onAlbumClick?: (album: AlbumRef) => void;
    onArtistClick?: (artist: ArtistRef) => void;
    onTrackClick?: (track: TrackMetadata) => void;
}
export function TrackList({ tracks, searchQuery, sortBy, currentPage, itemsPerPage, hideAlbumColumn = false, onPageChange, onAlbumClick, onArtistClick, onTrackClick, canDownload = false, downloadItems, onDownloadTrack, onRetryDownload, }: TrackListProps) {
    const { t, language } = useI18n();
    const { label } = useSources();
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
        return num.toLocaleString(language);
    };
    // Amazon exposes no play counts; an always-empty column is noise.
    const hasPlays = tracks.some((track) => track.plays);
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
                {t("table.title")}
              </th>
              {!hideAlbumColumn && (<th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden md:table-cell w-1/4">
                {t("type.album")}
              </th>)}
              {/* Sized for the longest translation (ru); truncate keeps a wider font from overlapping the next column. */}
              <th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden lg:table-cell w-36 truncate" title={t("track.duration")}>
                {t("track.duration")}
              </th>
              {hasPlays && (<th className="h-12 px-4 text-left align-middle font-medium text-muted-foreground hidden xl:table-cell w-40 truncate" title={t("track.plays")}>
                {t("track.plays")}
              </th>)}
              <th className="h-12 px-4 text-center align-middle font-medium text-muted-foreground w-32 truncate">
                {t("table.actions")}
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
                  <CoverPreview service={track.service} trackId={track.id} trackName={track.name} src={track.images} className="w-10 h-10"/>
                  <div className="flex min-w-0 flex-col [overflow-wrap:anywhere]">
                    <div className="flex items-center gap-2">
                      {onTrackClick ? (<span className="font-medium cursor-pointer hover:underline" onClick={() => onTrackClick(track)}>
                        {track.name}
                      </span>) : (<span className="font-medium">{track.name}</span>)}
                      {track.is_explicit && (<span className="inline-flex items-center justify-center bg-red-600 text-white text-[10px] h-4 w-4 rounded shrink-0" title={t("track.explicit")}>E</span>)}
                    </div>
                    <ArtistLinks track={track} onArtistClick={onArtistClick} className="text-sm text-muted-foreground"/>
                  </div>
                </div>
              </td>
              {!hideAlbumColumn && (<td className="p-4 align-middle text-sm text-muted-foreground hidden md:table-cell [overflow-wrap:anywhere]">
                <AlbumLink track={track} onAlbumClick={onAlbumClick}/>
              </td>)}
              <td className="p-4 align-middle text-sm text-muted-foreground hidden lg:table-cell">
                {formatDuration(track.duration_ms)}
              </td>
              {hasPlays && (<td className="p-4 align-middle text-sm text-muted-foreground hidden xl:table-cell">
                {track.plays ? formatPlays(track.plays) : ""}
              </td>)}
              <td className="p-4 align-middle text-center">
                <div className="flex items-center justify-center gap-1">
                  {onDownloadTrack && (<DownloadButton item={downloadItems?.get(track.id ?? "")} disabled={!canDownload} onDownload={() => onDownloadTrack(track, tracks.indexOf(track))} onRetry={() => track.id && onRetryDownload?.(track.id)}/>)}
                  {track.external_urls && (<Tooltip>
                    <TooltipTrigger asChild>
                      <Button onClick={() => openExternal(track.external_urls)} size="icon" variant="outline" aria-label={t("service.open", { service: label(track.service) })}>
                        <ExternalLink className="h-4 w-4"/>
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      <p>{t("service.open", { service: label(track.service) })}</p>
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
