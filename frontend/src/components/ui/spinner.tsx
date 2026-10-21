import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { useI18n } from "@/lib/i18n";
function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
    const { t } = useI18n();
    return (<Loader2 role="status" aria-label={t("common.loading")} className={cn("size-4 animate-spin", className)} {...props}/>);
}
export { Spinner };
