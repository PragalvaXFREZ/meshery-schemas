export const injectedRtkApi = {
  endpoints: (build) => ({
    getExamples: build.query<GetExamplesApiResponse, GetExamplesApiArg>({
      query: (queryArg) => ({
        url: `/api/examples`,
        params: {
          page: queryArg.page,
          type: queryArg["type"],
        },
      }),
    }),
    getDesigns: build.query<GetDesignsApiResponse, GetDesignsApiArg>({
      query: (queryArg) => ({
        url: `/api/designs/${queryArg.itemId}`,
        params: {
          filter: queryArg.filter,
          class: queryArg["class"],
        },
        body: queryArg.body,
      }),
    }),
  }),
};

export type GetExamplesApiResponse = unknown;
export type GetExamplesApiArg = {
  page?: string;
  type?: string;
};

export type GetDesignsApiResponse = unknown;
export type GetDesignsApiArg = {
  itemId: string;
  filter?: string;
  class?: string;
  body: {
    name: string;
  };
};
