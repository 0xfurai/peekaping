export const getConfig = () => {
  const config = (window as unknown as { __CONFIG__: { API_URL: string } })
    .__CONFIG__;

  return {
    API_URL: config?.API_URL ?? "",
  };
};
