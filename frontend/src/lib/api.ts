import type { MetadataResponse } from "@/types/api";
import { GetMetadata } from "../../wailsjs/go/main/App";
export async function fetchMetadata(url: string): Promise<MetadataResponse> {
    return JSON.parse(await GetMetadata(url));
}
