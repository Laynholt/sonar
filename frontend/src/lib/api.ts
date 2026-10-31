import type { MetadataResponse } from "@/types/api";
import { GetMetadata } from "../../wailsjs/go/main/App";
import { explainMissingPlugins } from "./plugins";
export async function fetchMetadata(url: string): Promise<MetadataResponse> {
    try {
        return JSON.parse(await GetMetadata(url));
    }
    catch (err) {
        throw explainMissingPlugins(err);
    }
}
