import { cn } from "@/lib/utils";

/** A thin bar; value is 0..1. */
export function Progress({ value, className }: { value: number; className?: string }) {
    const pct = Math.round(Math.min(1, Math.max(0, value)) * 100);
    return (<div role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} className={cn("h-1.5 w-full rounded-full bg-muted overflow-hidden", className)}>
      <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${pct}%` }}/>
    </div>);
}
