import { paginator, PaginatorTypes } from '@nodeteam/nestjs-prisma-pagination';

export const createPaginator = (defaultPage = 1, defaultPerPage = 15): PaginatorTypes.PaginateFunction => {
  return paginator({
    page: defaultPage,
    perPage: defaultPerPage,
  });
};

export const defaultPaginator = createPaginator();
