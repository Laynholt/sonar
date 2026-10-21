import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue, } from "@/components/ui/select";
import { Search, ArrowUpDown, XCircle } from "lucide-react";
import { useI18n } from "@/lib/i18n";
interface SearchAndSortProps {
    searchQuery: string;
    sortBy: string;
    hasPlays: boolean;
    onSearchChange: (value: string) => void;
    onSortChange: (value: string) => void;
}
export function SearchAndSort({ searchQuery, sortBy, hasPlays, onSearchChange, onSortChange, }: SearchAndSortProps) {
    const { t } = useI18n();
    return (<div className="flex gap-2">
      <div className="relative flex-1">
        <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground"/>
        <Input placeholder={t("list.search")} value={searchQuery} onChange={(e) => onSearchChange(e.target.value)} className="pl-10 pr-8"/>
        {searchQuery && (<button type="button" className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors cursor-pointer" onClick={() => onSearchChange("")}>
            <XCircle className="h-4 w-4"/>
          </button>)}
      </div>
      <Select value={sortBy} onValueChange={onSortChange}>
        <SelectTrigger className="w-[200px] gap-1.5">
          <ArrowUpDown className="h-4 w-4"/>
          <SelectValue placeholder={t("list.sortBy")}/>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="default">{t("sort.default")}</SelectItem>
          <SelectItem value="title-asc">{t("sort.titleAsc")}</SelectItem>
          <SelectItem value="title-desc">{t("sort.titleDesc")}</SelectItem>
          <SelectItem value="artist-asc">{t("sort.artistAsc")}</SelectItem>
          <SelectItem value="artist-desc">{t("sort.artistDesc")}</SelectItem>
          <SelectItem value="duration-asc">{t("sort.durationAsc")}</SelectItem>
          <SelectItem value="duration-desc">{t("sort.durationDesc")}</SelectItem>
          {hasPlays && <SelectItem value="plays-asc">{t("sort.playsAsc")}</SelectItem>}
          {hasPlays && <SelectItem value="plays-desc">{t("sort.playsDesc")}</SelectItem>}
        </SelectContent>
      </Select>
    </div>);
}
