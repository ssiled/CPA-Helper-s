import { apiClient } from '@/shared/api/apiClient'
import type {
  CardShopFavoritesResponse,
  CardShopFavoriteUpdatePayload,
  CardShopsResponse,
} from '@/shared/types/api'

export function getCardShops(): Promise<CardShopsResponse> {
  return apiClient.get<CardShopsResponse>('/card-shops')
}

export function getCardShopFavorites(): Promise<CardShopFavoritesResponse> {
  return apiClient.get<CardShopFavoritesResponse>('/card-shops/favorites')
}

export function updateCardShopFavorite(payload: CardShopFavoriteUpdatePayload): Promise<CardShopFavoritesResponse> {
  return apiClient.put<CardShopFavoritesResponse>('/card-shops/favorites', payload)
}
