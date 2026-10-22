import type { ReactNode } from "react";
import { HomeIcon } from "@/components/ui/home";
import { HistoryIcon } from "@/components/ui/history-icon";
import { SettingsIcon } from "@/components/ui/settings";
import { TerminalIcon } from "@/components/ui/terminal";
import { BadgeAlertIcon } from "@/components/ui/badge-alert";
import { Tooltip, TooltipContent, TooltipTrigger, } from "@/components/ui/tooltip";
import { Download } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useI18n, type MessageKey } from "@/lib/i18n";

export type PageType = "main" | "history" | "downloads" | "settings" | "debug" | "about";
interface SidebarProps {
    currentPage: PageType;
    onPageChange: (page: PageType) => void;
    activeCount: number;
}

type Item = { page: PageType; label: MessageKey; icon: ReactNode };
const topItems: Item[] = [
    { page: "main", label: "nav.home", icon: <HomeIcon size={20}/> },
    { page: "history", label: "nav.history", icon: <HistoryIcon size={20}/> },
    { page: "downloads", label: "nav.downloads", icon: <Download size={20}/> },
    { page: "debug", label: "nav.debug", icon: <TerminalIcon size={20} loop={true}/> },
    { page: "settings", label: "nav.settings", icon: <SettingsIcon size={20}/> },
];
const aboutItem: Item = { page: "about", label: "nav.about", icon: <BadgeAlertIcon size={20}/> };

export function Sidebar({ currentPage, onPageChange, activeCount }: SidebarProps) {
    const { t } = useI18n();
    const item = ({ page, label: key, icon }: Item) => {
        const label = t(key);
        const active = currentPage === page;
        return (<Tooltip key={page} delayDuration={0}>
        <TooltipTrigger asChild>
          <Button aria-label={page === "downloads" && activeCount > 0 ? `${label} (${activeCount})` : label} aria-current={active ? "page" : undefined} variant={active ? "secondary" : "ghost"} size="icon" className={`relative h-10 w-10 ${active ? "bg-primary/10 text-primary hover:bg-primary/20" : "hover:bg-primary/10 hover:text-primary"}`} onClick={() => onPageChange(page)}>
            {icon}
            {page === "downloads" && activeCount > 0 && (<Badge className="absolute -right-1 -top-1 h-4 min-w-4 justify-center px-1 text-[10px] font-mono">{activeCount}</Badge>)}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="right">
          <p>{label}</p>
        </TooltipContent>
      </Tooltip>);
    };
    return (<div className="fixed left-0 top-0 h-full w-14 bg-card border-r border-border flex flex-col items-center py-6 z-30">
    <div className="flex flex-col gap-2 flex-1">{topItems.map(item)}</div>
    <div className="mt-auto flex flex-col gap-2">{item(aboutItem)}</div>
  </div>);
}
