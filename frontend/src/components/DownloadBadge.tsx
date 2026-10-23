import type { PointerEvent, Ref } from "react";
import { Check } from "lucide-react";
import { useI18n } from "@/lib/i18n";

interface Props {
    ref: Ref<HTMLButtonElement>;
    progress: number;
    speed: string;
    done: boolean;
    attention: boolean;
    onExpand: () => void;
    onPointerDown: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerMove: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerUp: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerCancel: (e: PointerEvent<HTMLButtonElement>) => void;
    className: string;
}

/** The collapsed download widget: a square whose outline fills with progress, showing the speed, or a check once finished. */
export function DownloadBadge({ ref, progress, speed, done, attention, onExpand, className, ...rest }: Props) {
    const { t } = useI18n();
    return (<button ref={ref} type="button" {...rest} aria-label={done ? t("widget.badgeDoneLabel") : t("widget.badgeLabel", { percent: Math.round(progress * 100), speed })}
        className={`${className} flex h-[60px] w-[60px] items-center justify-center rounded-[12px] bg-neutral-900 text-white shadow-lg cursor-grab active:cursor-grabbing select-none touch-none`}
        onClick={(e) => { if (e.detail === 0) onExpand(); }}>
      {/* pathLength=100 makes the dash a plain percentage of the outline; the outline starts at the top edge. */}
      <svg viewBox="0 0 60 60" className="absolute inset-0" aria-hidden>
        <rect x="2" y="2" width="56" height="56" rx="10" fill="none" stroke="#444441" strokeWidth="4"/>
        <rect x="2" y="2" width="56" height="56" rx="10" fill="none" stroke="#5DCAA5" strokeWidth="4" pathLength={100}
            strokeDasharray={100} strokeDashoffset={100 * (1 - Math.min(1, progress))}/>
      </svg>
      {done
        ? <Check data-testid="badge-done" className="relative h-6 w-6 text-[#5DCAA5]"/>
        : <span className="relative flex flex-col items-center text-[11px] leading-tight tabular-nums"><span>{speed}</span><span className="text-[9px] text-neutral-400">{t("widget.badgeSpeedUnit")}</span></span>}
      {attention && <span data-testid="badge-attention" className="absolute -right-1 -top-1 h-3 w-3 rounded-full bg-amber-500 ring-2 ring-neutral-900"/>}
    </button>);
}
