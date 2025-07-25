import type { LayoutServerLoad } from './$types';
import { api } from '$lib/api';

export interface SessionUser {
    slug: string,
    displayName: string,
}

export const load: LayoutServerLoad = async ({ fetch }) => {
    const session = await api.with(fetch).getCurrentSession()

    if (session === null) {
        return {
            hasSession: false,
            session: {
                slug: "",
                displayName: "",
            },
        }
    }

    return {
        hasSession: true,
        session: session,
    }
}
