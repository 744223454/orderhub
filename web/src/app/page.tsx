import { redirect } from 'next/navigation';

/**
 * 根路由。
 *
 * 这里刻意什么都不渲染，直接重定向到 /orders：
 * 服务端读不到 localStorage，也就无法在这一层判断登录态，
 * 交给 (user)/layout.tsx 的守卫决定是留下还是转去 /login。
 * 这样"按角色分流"的逻辑只存在于守卫与登录页两处，不会散落各处。
 *
 * 原先那个 antd 自检按钮页已完成使命，删掉了——
 * 登录页本身就是更好的自检：表单能渲染、能提交、能报错，说明整条链路是通的。
 */
export default function RootPage() {
  redirect('/orders');
}
