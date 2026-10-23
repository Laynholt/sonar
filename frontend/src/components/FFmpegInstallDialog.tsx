import { useEffect, useRef, useState } from "react";
import { CircleCheck, Download, PackageOpen, TriangleAlert } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Progress } from "@/components/ui/progress";
import { eta, mb } from "@/lib/download-format";
import { closeFFmpegInstall, useFFmpegInstall, type FFmpegInstallRequest } from "@/lib/ffmpeg-install";
import { useI18n } from "@/lib/i18n";
import { logger } from "@/lib/logger";
import { updateSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { cn, errorMessage } from "@/lib/utils";
import { CancelFFmpegInstall, DefaultFFmpegDir, GetFFmpegInstall, SelectFFmpegFolder, StartFFmpegInstall } from "../../wailsjs/go/main/App";

const POLL_MS = 250;
const MB = 1024 * 1024;

type Step = "ask" | "download" | "install" | "done" | "error";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/** The FFmpeg install dialog; it opens through openFFmpegInstall. */
export function FFmpegInstallDialog() {
    const request = useFFmpegInstall();
    return request && <Installer key={request.id} request={request}/>;
}

function Installer({ request }: { request: FFmpegInstallRequest }) {
    const { t, language } = useI18n();
    const [step, setStep] = useState<Step>("ask");
    const [dir, setDir] = useState("");
    const [fallback, setFallback] = useState(false);
    const [progress, setProgress] = useState({ done: 0, total: 0 });
    const [speed, setSpeed] = useState(0);
    const [failure, setFailure] = useState({ message: "", notEmpty: false });
    const [path, setPath] = useState("");
    // Cleared on unmount: the poll loop stops there, which is how closing the modal stops polling.
    const alive = useRef(true);
    const busy = useRef(false);
    const finished = useRef(false);
    const running = step === "download" || step === "install";

    useEffect(() => {
        alive.current = true;
        DefaultFFmpegDir().then((found) => {
            if (!alive.current) return;
            setDir((typed) => typed || found.dir);
            setFallback(found.fallback);
        }, (err) => logger.error(errorMessage(err, "FFmpeg default folder failed")));
        return () => { alive.current = false; };
    }, []);

    const fail = (message: string, notEmpty = false) => {
        busy.current = false;
        setFailure({ message, notEmpty });
        setStep("error");
    };

    // The one way an install ends well, from the poll loop or from a Cancel that found it already done.
    async function finish(installed: string) {
        if (finished.current) return;
        finished.current = true;
        try { await updateSettings({ ffmpegPath: installed }); }
        catch (err) { fail(errorMessage(err, t("recent.unknownError"))); return; }
        busy.current = false;
        setPath(installed);
        setStep("done");
        request.onInstalled?.(installed);
    }

    async function start() {
        if (busy.current) return;
        busy.current = true;
        finished.current = false;
        setStep("download");
        setProgress({ done: 0, total: 0 });
        setSpeed(0);
        try { await StartFFmpegInstall(dir.trim()); }
        catch (err) { fail(errorMessage(err, t("recent.unknownError"))); return; }
        let last = { bytes: 0, at: Date.now() }, smooth = 0;
        while (alive.current) {
            await sleep(POLL_MS);
            if (!alive.current) return;
            let state;
            try { state = await GetFFmpegInstall(); }
            catch (err) { fail(errorMessage(err, t("recent.unknownError"))); return; }
            if (!alive.current) return;
            if (state.step === "idle") continue;
            if (state.step === "error") {
                if (state.error === "cancelled") closeFFmpegInstall();
                else fail(state.error || t("recent.unknownError"), state.not_empty);
                return;
            }
            if (state.step === "done") {
                await finish(state.path);
                return;
            }
            setStep(state.step as Step);
            setProgress({ done: state.done, total: state.total });
            const now = Date.now();
            smooth = 0.5 * smooth + 0.5 * Math.max(0, state.done - last.bytes) / Math.max(0.001, (now - last.at) / 1000);
            last = { bytes: state.done, at: now };
            setSpeed(state.step === "download" ? smooth : 0);
        }
    }

    // An install that finished before the cancel landed stays done, so look once more before closing.
    const cancel = async () => {
        try {
            await CancelFFmpegInstall();
            const state = await GetFFmpegInstall();
            if (state.step === "done") { await finish(state.path); return; }
        }
        catch (err) { logger.error(errorMessage(err, "FFmpeg cancel failed")); }
        closeFFmpegInstall();
    };
    const browse = async () => {
        try {
            const picked = await SelectFFmpegFolder();
            if (picked) setDir(picked);
        }
        catch (err) { toast.error(errorMessage(err, t("recent.unknownError"))); }
    };

    const Icon = { ask: Download, download: Download, install: PackageOpen, done: CircleCheck, error: TriangleAlert }[step];
    const title = { ask: "ffmpeg.title", download: "ffmpeg.downloading", install: "ffmpeg.installing", done: "ffmpeg.done", error: "ffmpeg.failed" }[step] as Parameters<typeof t>[0];
    const stepNo = step === "download" ? 1 : step === "install" ? 2 : 0;
    const ratio = progress.total > 0 ? progress.done / progress.total : 0;
    const megabytes = (bytes: number) => Math.round(bytes / MB).toLocaleString(language);
    const left = eta(progress.total - progress.done, speed);
    const folderField = (<div className="space-y-2">
      <Label htmlFor="ffmpeg-dir">{t("ffmpeg.where")}</Label>
      <div className="flex gap-2">
        <Input id="ffmpeg-dir" value={dir} onChange={(e) => setDir(e.target.value)} className="font-mono"/>
        <Button type="button" variant="outline" onClick={() => void browse()}>{t("ffmpeg.browse")}</Button>
      </div>
      {fallback && <p className="text-xs text-muted-foreground">{t("ffmpeg.fallbackNote")}</p>}
    </div>);

    return (<Dialog open onOpenChange={(open) => { if (!open) (running ? () => void cancel() : closeFFmpegInstall)(); }}>
      <DialogContent showCloseButton={false} aria-describedby={undefined} className="max-w-[520px]"
          onEscapeKeyDown={(e) => running && e.preventDefault()} onInteractOutside={(e) => running && e.preventDefault()}>
        <DialogHeader className="flex-row items-center gap-3 text-left">
          <div className={cn("flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-muted", step === "error" && "text-destructive", step === "done" && "text-primary")}>
            <Icon className="h-5 w-5"/>
          </div>
          <div className="min-w-0">
            <DialogTitle>{t(title)}</DialogTitle>
            {stepNo > 0 && <p className="mt-1 text-xs text-muted-foreground">{t("ffmpeg.step", { n: stepNo })}</p>}
          </div>
        </DialogHeader>

        {step === "ask" && (<>
          <p className="text-sm text-muted-foreground">{t("ffmpeg.desc")}</p>
          {folderField}
        </>)}

        {running && (<div className="space-y-2">
          <Progress value={ratio} className="h-2"/>
          <div className="flex justify-between text-xs text-muted-foreground tabular-nums">
            <span>{step === "download"
                ? [t("ffmpeg.size", { done: megabytes(progress.done), total: megabytes(progress.total) }), speed > 0 && t("ffmpeg.speed", { speed: mb(speed, language) })].filter(Boolean).join(" · ")
                : t("ffmpeg.unpacking", { done: progress.done, total: progress.total })}</span>
            <span>{step === "download" && left && t("ffmpeg.left", { time: left })}</span>
          </div>
        </div>)}

        {step === "done" && <p className="break-all font-mono text-xs text-muted-foreground">{path}</p>}

        {step === "error" && (<>
          <p className="text-sm text-destructive">{failure.notEmpty ? t("ffmpeg.notEmpty") : failure.message}</p>
          {failure.notEmpty && folderField}
        </>)}

        <DialogFooter>
          {step === "ask" && (<>
            <Button variant="outline" onClick={closeFFmpegInstall}>{t("ffmpeg.later")}</Button>
            <Button disabled={!dir.trim()} onClick={() => void start()}>{t("ffmpeg.install")}</Button>
          </>)}
          {running && <Button variant="outline" onClick={() => void cancel()}>{t("common.cancel")}</Button>}
          {step === "done" && <Button onClick={closeFFmpegInstall}>{t("common.close")}</Button>}
          {step === "error" && (<>
            <Button variant="outline" onClick={closeFFmpegInstall}>{t("common.close")}</Button>
            <Button onClick={() => void start()}>{t("ffmpeg.retry")}</Button>
          </>)}
        </DialogFooter>
      </DialogContent>
    </Dialog>);
}
