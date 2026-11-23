import { createRouter, createWebHistory } from 'vue-router'

// 页面级懒加载（dev.md 12.2）。
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'gallery', component: () => import('@/views/GalleryView.vue') },
    { path: '/timeline', name: 'timeline', component: () => import('@/views/TimelineView.vue') },
    { path: '/similar', name: 'similar', component: () => import('@/views/SimilarView.vue') },
    { path: '/albums', name: 'albums', component: () => import('@/views/AlbumsView.vue') },
    {
      path: '/albums/:id',
      name: 'album-detail',
      component: () => import('@/views/AlbumDetailView.vue'),
      props: true,
    },
    { path: '/tags', name: 'tags', component: () => import('@/views/TagsView.vue') },
    { path: '/import', name: 'import', component: () => import('@/views/ImportView.vue') },
    { path: '/trash', name: 'trash', component: () => import('@/views/TrashView.vue') },
    { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

export default router
