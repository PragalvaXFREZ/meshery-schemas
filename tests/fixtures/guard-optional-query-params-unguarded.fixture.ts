export const injectedRtkApi = {
  endpoints: (build) => ({
    getExamples: build.query<GetExamplesApiResponse, GetExamplesApiArg>({
      query: (queryArg) => ({
        url: `/api/examples`,
        params: {
          page: queryArg
            .page,
        },
      }),
    }),
  }),
};

export type GetExamplesApiResponse = unknown;
export type GetExamplesApiArg = {
  page?: string;
};
