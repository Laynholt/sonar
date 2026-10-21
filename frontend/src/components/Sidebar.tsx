import type { ReactNode } from "react";
import { HomeIcon } from "@/components/ui/home";
import { HistoryIcon } from "@/components/ui/history-icon";
import { SettingsIcon } from "@/components/ui/settings";
import { TerminalIcon } from "@/components/ui/terminal";
import { BadgeAlertIcon } from "@/components/ui/badge-alert";
import { Tooltip, TooltipContent, TooltipTrigger, } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";

export type PageType = "main" | "history" | "settings" | "debug" | "about";
interface SidebarProps {
    currentPage: PageType;
    onPageChange: (page: PageType) => void;
}

type Item = { page: PageType; label: string; icon: ReactNode };
const topItems: Item[] = [
    { page: "main", label: "Home", icon: <HomeIcon size={20}/> },
    { page: "history", label: "History", icon: <HistoryIcon size={20}/> },
    { page: "debug", label: "Debug Logs", icon: <TerminalIcon size={20} loop={true}/> },
    { page: "settings", label: "Settings", icon: <SettingsIcon size={20}/> },
];
const aboutItem: Item = { page: "about", label: "About", icon: <BadgeAlertIcon size={20}/> };

export function Sidebar({ currentPage, onPageChange }: SidebarProps) {
    const item = ({ page, label, icon }: Item) => {
        const active = currentPage === page;
        return (<Tooltip key={page} delayDuration={0}>
        <TooltipTrigger asChild>
          <Button aria-label={label} aria-current={active ? "page" : undefined} variant={active ? "secondary" : "ghost"} size="icon" className={`h-10 w-10 ${active ? "bg-primary/10 text-primary hover:bg-primary/20" : "hover:bg-primary/10 hover:text-primary"}`} onClick={() => onPageChange(page)}>
            {icon}
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
