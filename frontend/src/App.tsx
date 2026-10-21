import { useState, useEffect, useLayoutEffect, useCallback, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { TooltipProvider } from "@/components/ui/tooltip";
import { getSettings, getSettingsLoadError, loadSettings, applyThemeMode, applyFont } from "@/lib/settings";
import { applyTheme } from "@/lib/themes";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { TitleBar } from "@/components/TitleBar";
import { Sidebar, type PageType } from "@/components/Sidebar";
import { Header } from "@/components/Header";
import { SettingsPage } from "@/components/SettingsPage";
import { DebugLoggerPage } from "@/components/DebugLoggerPage";
import { AboutPage } from "@/components/AboutPage";
import { HistoryPage } from "@/components/HistoryPage";

function applySavedAppearance() {
    const settings = getSettings();
    applyThemeMode(settings.themeMode);
    applyTheme(settings.theme);
    applyFont(settings.fontFamily);
}

function App() {
    const [currentPage, setCurrentPage] = useState<PageType>("main");
    const [hasUnsavedSettings, setHasUnsavedSettings] = useState(false);
    const [pendingPageChange, setPendingPageChange] = useState<PageType | null>(null);
    const [showUnsavedChangesDialog, setShowUnsavedChangesDialog] = useState(false);
    const resetSettingsFn = useRef<(() => void) | null>(null);
    const rememberSettingsReset = useCallback((reset: () => void) => { resetSettingsFn.current = reset; }, []);
    useLayoutEffect(applySavedAppearance, []);
    useEffect(() => {
        void loadSettings().then(() => {
            applySavedAppearance();
            const loadError = getSettingsLoadError();
            if (loadError) {
                toast.error("Settings file could not be read; using the last local copy", { description: String(loadError) });
            }
        });
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            if (getSettings().themeMode === "auto") applySavedAppearance();
        };
        mediaQuery.addEventListener("change", handleChange);
        return () => mediaQuery.removeEventListener("change", handleChange);
    }, []);
    const handlePageChange = (page: PageType) => {
        if (currentPage === "settings" && hasUnsavedSettings && page !== "settings") {
            setPendingPageChange(page);
            setShowUnsavedChangesDialog(true);
            return;
        }
        setCurrentPage(page);
    };
    const handleDiscardChanges = () => {
        setShowUnsavedChangesDialog(false);
        resetSettingsFn.current?.();
        applySavedAppearance();
        if (pendingPageChange) {
            setCurrentPage(pendingPageChange);
            setPendingPageChange(null);
        }
    };
    const handleCancelNavigation = () => {
        setShowUnsavedChangesDialog(false);
        setPendingPageChange(null);
    };
    const renderPage = () => {
        switch (currentPage) {
            case "settings":
                return <SettingsPage onUnsavedChangesChange={setHasUnsavedSettings} onResetRequest={rememberSettingsReset}/>;
            case "debug":
                return <DebugLoggerPage />;
            case "about":
                return <AboutPage />;
            case "history":
                return <HistoryPage />;
            default:
                return (<>
                    <Header />
                    {/* The Spotify search and result cards are mounted here. */}
                </>);
        }
    };
    return (<TooltipProvider>
        <div className="min-h-screen bg-background flex flex-col">
            <TitleBar />
            <Sidebar currentPage={currentPage} onPageChange={handlePageChange}/>

            <div className="flex-1 ml-14 mt-10 p-4 md:p-8">
                <div className={`mx-auto space-y-6 ${currentPage === "main" ? "w-full" : "max-w-4xl"}`}>
                    {renderPage()}
                </div>
            </div>

            <Dialog open={showUnsavedChangesDialog} onOpenChange={setShowUnsavedChangesDialog}>
                <DialogContent className="sm:max-w-[425px] [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>Unsaved Changes</DialogTitle>
                        <DialogDescription>
                            You have unsaved changes in Settings. Are you sure you want to leave? Your changes will be lost.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={handleCancelNavigation}>
                            Cancel
                        </Button>
                        <Button variant="destructive" onClick={handleDiscardChanges}>
                            Discard Changes
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    </TooltipProvider>);
}
export default App;
