import type { ReactNode, SVGProps } from 'react';

/**
 * Icon dung inline SVG, khong dung ky tu.
 *
 * Truoc day cac cho nay la mot ky tu tich hoac mot ky tu mui viet truc tiep trong
 * JSX. Ky tu do font he thong ve, nen no doi hinh theo may (Windows ve dau tich
 * mong va lech xuong duoi baseline, macOS ve day va can giua), khong theo duoc do
 * day net cua thiet ke, va khong can duoc theo baseline chu ben canh. SVG thi kich
 * thuoc do `className`, mau theo `currentColor`, va giong nhau tren moi may.
 *
 * Do day net 1.75 tren khung 16 khop voi net cua cac duong ke trong console.
 *
 * Tat ca icon o day deu `aria-hidden`: chung luon di kem mot nhan chu, nen doc lai
 * chung chi lam trinh doc man hinh noi hai lan cung mot y.
 */

type IconProps = Omit<SVGProps<SVGSVGElement>, 'children' | 'viewBox'> & {
  className?: string;
};

function Icon({
  children,
  className = 'size-4',
  ...rest
}: IconProps & { children: ReactNode }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...rest}
    >
      {children}
    </svg>
  );
}

/** Dau tich: buoc da hoan thanh. */
export function CheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3.25 8.5 6.25 11.5l6.5-7" />
    </Icon>
  );
}

/** Mui sang phai: di tiep trong cung mot luong. */
export function ArrowRightIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.75 8h10.5" />
      <path d="M9.25 4 13.25 8l-4 4" />
    </Icon>
  );
}

/** Mui cheo len: mo ra ngoai - trang khac hoac tab moi. */
export function ArrowUpRightIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4.75 11.25 11.25 4.75" />
      <path d="M5.75 4.75h5.5v5.5" />
    </Icon>
  );
}

/** Dau nhan cho nut dong. Khong dung `\u00d7`: do la ky tu toan hoc, khong phai icon. */
export function CloseIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 4l8 8" />
      <path d="M12 4l-8 8" />
    </Icon>
  );
}

/** Bieu tuong sao chep hai lop giay chong len nhau. */
export function CopyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="5.5" y="5.5" width="7.5" height="7.5" rx="1.5" />
      <path d="M10.5 3.5H4a1.5 1.5 0 0 0-1.5 1.5v6.5" />
    </Icon>
  );
}

/** Bieu tuong che do tuong phan cao (nua sang nua toi). */
export function ContrastIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="8" cy="8" r="5.75" />
      <path d="M8 2.25v11.5a5.75 5.75 0 0 0 0-11.5z" fill="currentColor" />
    </Icon>
  );
}


