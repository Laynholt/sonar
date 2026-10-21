import { Badge } from "@/components/ui/badge";
import type { Service } from "@/types/api";
const labels: Record<Service, string> = { spotify: "Spotify", amazon: "Amazon Music" };
export function ServiceBadge({ service }: {
    service: Service;
}) {
    return <Badge variant="outline">{labels[service]}</Badge>;
}
