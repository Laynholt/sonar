import { Badge } from "@/components/ui/badge";
import type { Service } from "@/types/api";
export const serviceLabels: Record<Service, string> = { spotify: "Spotify", amazon: "Amazon Music" };
// Link mode accepts both services, so its copy names both.
export const linkServices = `${serviceLabels.spotify} / ${serviceLabels.amazon}`;
export function ServiceBadge({ service }: {
    service: Service;
}) {
    return <Badge variant="outline">{serviceLabels[service]}</Badge>;
}
