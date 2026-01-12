import { useEffect } from "react";
import { useSmartRedirect } from "@/hooks/use-smart-redirect";

export const RootRedirect = () => {
    const { handleRedirect } = useSmartRedirect();

    useEffect(() => {
        handleRedirect();
    }, [handleRedirect]);

    return <div className="flex items-center justify-center h-screen">Loading...</div>;
};
