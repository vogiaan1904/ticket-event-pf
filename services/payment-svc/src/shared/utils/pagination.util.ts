export interface PaginationOptions {
  page?: number;
  perPage?: number;
}

export interface PaginatedResult<T> {
  data: T[];
  meta: {
    total: number;
    lastPage: number;
    currentPage: number;
    perPage: number;
    prev: number | null;
    next: number | null;
  };
}

export type PaginateFunction = <T, K>(
  model: any,
  args?: K,
  options?: PaginationOptions,
) => Promise<PaginatedResult<T>>;

export const createPaginator = (defaultPage = 1, defaultPerPage = 15): PaginateFunction => {
  return async <T, K>(model: any, args: K = {} as K, options?: PaginationOptions) => {
    const page = Number(options?.page || defaultPage);
    const perPage = Number(options?.perPage || defaultPerPage);

    const skip = page > 0 ? perPage * (page - 1) : 0;

    const [total, data] = await Promise.all([
      model.count({ where: (args as any)?.where }),
      model.findMany({
        ...args,
        take: perPage,
        skip,
      }),
    ]);

    const lastPage = Math.ceil(total / perPage);

    return {
      data,
      meta: {
        total,
        lastPage,
        currentPage: page,
        perPage,
        prev: page > 1 ? page - 1 : null,
        next: page < lastPage ? page + 1 : null,
      },
    };
  };
};

export const defaultPaginator = createPaginator();
