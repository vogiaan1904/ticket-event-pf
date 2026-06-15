export interface PaginationQuery {
  page?: number;
  perPage?: number;
}

export interface BaseRepositoryInterface<T> {
  create(data: any, options?: any): Promise<T>;
  update(id: string, data: any, options?: any): Promise<T>;
  findOne(filter: any, options?: any): Promise<T>;
  findAll(filter: any, orderBy?: any, options?: any): Promise<T[]>;
  delete(id: string): Promise<T>;
}
